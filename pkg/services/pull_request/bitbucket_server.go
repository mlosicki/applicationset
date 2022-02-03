package pull_request

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	bitbucketv1 "github.com/gfleury/go-bitbucket-v1"
	log "github.com/sirupsen/logrus"
)

type BitbucketService struct {
	client               *bitbucketv1.APIClient
	projectKey           string
	repositorySlug       string
	branchMatch          *regexp.Regexp
	successfulBuilds     []regexp.Regexp
	findLatestSuccessful bool
	// Not supported for PRs by Bitbucket Server
	// labels         []string
}

const SUCCESSFUL = "SUCCESSFUL"

var _ PullRequestService = (*BitbucketService)(nil)

func NewBitbucketServiceBasicAuth(ctx context.Context, username, password, url, projectKey, repositorySlug string, branchMatch *string, successfulBuilds []string, findLatestSuccessful bool) (PullRequestService, error) {
	bitbucketConfig := bitbucketv1.NewConfiguration(url)
	// Avoid the XSRF check
	bitbucketConfig.AddDefaultHeader("x-atlassian-token", "no-check")
	bitbucketConfig.AddDefaultHeader("x-requested-with", "XMLHttpRequest")

	ctx = context.WithValue(ctx, bitbucketv1.ContextBasicAuth, bitbucketv1.BasicAuth{
		UserName: username,
		Password: password,
	})
	return newBitbucketService(ctx, bitbucketConfig, projectKey, repositorySlug, branchMatch, successfulBuilds, findLatestSuccessful)
}

func NewBitbucketServiceNoAuth(ctx context.Context, url, projectKey, repositorySlug string, branchMatch *string, successfulBuilds []string, findLatestSuccessful bool) (PullRequestService, error) {
	return newBitbucketService(ctx, bitbucketv1.NewConfiguration(url), projectKey, repositorySlug, branchMatch, successfulBuilds, findLatestSuccessful)
}

func newBitbucketService(ctx context.Context, bitbucketConfig *bitbucketv1.Configuration, projectKey, repositorySlug string, branchMatch *string, successfulBuilds []string, findLatestSuccessful bool) (PullRequestService, error) {
	if !strings.HasSuffix(bitbucketConfig.BasePath, "/rest") {
		bitbucketConfig.BasePath = bitbucketConfig.BasePath + "/rest"
	}
	bitbucketClient := bitbucketv1.NewAPIClient(ctx, bitbucketConfig)

	var branchMatchRegexp *regexp.Regexp
	if branchMatch != nil {
		var err error
		branchMatchRegexp, err = regexp.Compile(*branchMatch)
		if err != nil {
			return nil, fmt.Errorf("error compiling BranchMatch regexp %q: %v", *branchMatch, err)
		}
	}

	// nil represents no check, empty array is all builds found on commit
	var successfulBuildsRegexp []regexp.Regexp
	if successfulBuilds != nil {
		successfulBuildsRegexp = []regexp.Regexp{}
		for _, buildName := range successfulBuilds {
			buildNameRegexp, err := regexp.Compile(buildName)
			if err != nil {
				return nil, fmt.Errorf("error compiling build name regexp %s: %v", buildName, err)
			}
			successfulBuildsRegexp = append(successfulBuildsRegexp, *buildNameRegexp)
		}
	}

	return &BitbucketService{
		client:               bitbucketClient,
		projectKey:           projectKey,
		repositorySlug:       repositorySlug,
		branchMatch:          branchMatchRegexp,
		successfulBuilds:     successfulBuildsRegexp,
		findLatestSuccessful: findLatestSuccessful,
	}, nil
}

func (b *BitbucketService) List(_ context.Context) ([]*PullRequest, error) {
	paged := map[string]interface{}{
		"limit": 100,
	}

	pullRequests := []*PullRequest{}
	for {
		response, err := b.client.DefaultApi.GetPullRequestsPage(b.projectKey, b.repositorySlug, paged)
		if err != nil {
			return nil, fmt.Errorf("error listing pull requests for %s/%s: %v", b.projectKey, b.repositorySlug, err)
		}
		pulls, err := bitbucketv1.GetPullRequestsResponse(response)
		if err != nil {
			return nil, fmt.Errorf("error parsing pull request response %s: %v", response.Values, err)
		}

		for _, pull := range pulls {
			if b.branchMatch != nil && !b.branchMatch.MatchString(pull.FromRef.DisplayID) {
				log.Debugf("Branch %s does not match the pattern", pull.FromRef.DisplayID)
				continue
			}
			var headSHA = pull.FromRef.LatestCommit // This is not defined in the official docs, but works in practice
			if b.successfulBuilds != nil {
				allGreen, err := b.isCommitGreen(headSHA)
				if err != nil {
					return nil, err
				}
				if !allGreen {
					if !b.findLatestSuccessful {
						// Skip this PR as the builds are not green and we don't want to find the latest green commit
						log.Debugf("Commit %s has failed builds, skipping PR-%d", headSHA, pull.ID)
						continue
					} else {
						commits, err := b.getPullRequestCommits(pull.ID)
						if err != nil {
							return nil, err
						}
						var foundGreenCommit = false
						for _, commit := range commits {
							if commit.ID == pull.FromRef.ID {
								// In theory, it's possible that it isn't commits[0]
								continue
							}
							foundGreenCommit, err = b.isCommitGreen(commit.ID)
							if err != nil {
								return nil, err
							}
							if foundGreenCommit {
								log.Debugf("Latest green commit is %s", commit.ID)
								headSHA = commit.ID
								break
							}
						}
						if !foundGreenCommit {
							log.Debugf("Couldn't find a commit with successful builds, skipping PR-%d", pull.ID)
							continue
						}
					}
				}
			}

			pullRequests = append(pullRequests, &PullRequest{
				Number:  pull.ID,
				Branch:  pull.FromRef.DisplayID, // FromRef.ID: refs/heads/main FromRef.DisplayID: main
				HeadSHA: headSHA,
			})
		}

		hasNextPage, nextPageStart := bitbucketv1.HasNextPage(response)
		if !hasNextPage {
			break
		}
		paged["start"] = nextPageStart
	}
	return pullRequests, nil
}

// Return true if all builds are green
func verifyAllBuildsSuccessful(buildStatuses []bitbucketv1.BuildStatus) bool {
	for _, buildStatus := range buildStatuses {
		if buildStatus.State != SUCCESSFUL { // INPROGRESS is not green
			return false
		}
	}
	return true
}

// Return true if all the given regex build names are matching and green. Builds not listed are ignored.
func verifyListedBuildsSuccessful(successfulBuilds []regexp.Regexp, buildStatuses []bitbucketv1.BuildStatus) bool {
	for _, buildName := range successfulBuilds {
		green := isMatchingBuildSuccessful(buildName, buildStatuses)
		if !green {
			return false
		}
	}
	return true
}

// Return true if the build name matches the given regex AND the build is green
func isMatchingBuildSuccessful(buildName regexp.Regexp, buildStatuses []bitbucketv1.BuildStatus) bool {
	for _, buildStatus := range buildStatuses {
		if buildName.MatchString(buildStatus.Name) && buildStatus.State == SUCCESSFUL {
			return true
		}
	}
	return false
}

// Fetch all the build statuses associated with the commit SHA
func (b *BitbucketService) getBuildStatuses(commitId string) ([]bitbucketv1.BuildStatus, error) {
	paged := map[string]interface{}{
		"limit": 100,
	}

	res := []bitbucketv1.BuildStatus{}
	for {
		response, err := b.client.DefaultApi.GetCommitBuildStatuses(commitId)
		if err != nil {
			return nil, fmt.Errorf("error listing build statuses for %s: %v", commitId, err)
		}
		buildStatuses, err := bitbucketv1.GetBuildStatusesResponse(response)
		if err != nil {
			return nil, fmt.Errorf("error parsing build statuses response %s: %v", response.Values, err)
		}

		res = append(res, buildStatuses...)

		hasNextPage, nextPageStart := bitbucketv1.HasNextPage(response)
		if !hasNextPage {
			break
		}
		paged["start"] = nextPageStart
	}
	return res, nil
}

func (b *BitbucketService) isCommitGreen(commitId string) (bool, error) {
	buildStatuses, err := b.getBuildStatuses(commitId)
	if err != nil {
		return false, err
	}
	var allGreen bool
	if len(b.successfulBuilds) != 0 {
		allGreen = verifyListedBuildsSuccessful(b.successfulBuilds, buildStatuses)
	} else {
		allGreen = verifyAllBuildsSuccessful(buildStatuses)
	}
	return allGreen, nil
}

func (b *BitbucketService) getPullRequestCommits(pullRequestId int) ([]bitbucketv1.Commit, error) {
	paged := map[string]interface{}{
		"limit": 100,
	}

	res := []bitbucketv1.Commit{}
	for {
		response, err := b.client.DefaultApi.GetPullRequestCommitsWithOptions(b.projectKey, b.repositorySlug, pullRequestId, paged)
		if err != nil {
			return nil, fmt.Errorf("error listing pull request commits for %s/%s PR: %d: %v", b.projectKey, b.repositorySlug, pullRequestId, err)
		}
		commits, err := bitbucketv1.GetCommitsResponse(response)
		if err != nil {
			return nil, fmt.Errorf("error parsing pull request commits response %s: %v", response.Values, err)
		}

		res = append(res, commits...)

		hasNextPage, nextPageStart := bitbucketv1.HasNextPage(response)
		if !hasNextPage {
			break
		}
		paged["start"] = nextPageStart
	}
	return res, nil
}
