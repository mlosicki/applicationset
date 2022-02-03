package pull_request

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func defaultHandler(t *testing.T) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var err error
		switch r.RequestURI {
		case "/rest/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100":
			_, err = io.WriteString(w, `{
					"size": 1,
					"limit": 100,
					"isLastPage": true,
					"values": [
						{
							"id": 101,
							"fromRef": {
								"id": "refs/heads/feature-ABC-123",
								"displayId": "feature-ABC-123",
								"latestCommit": "cb3cf2e4d1517c83e720d2585b9402dbef71f992"
							}
						}
					],
					"start": 0
				}`)
		// TODO the cli library doesn't support "limit" query param for this API call, even though it is paginated in the docs
		// 	 The server default seems to be 25, so it's ok for now
		case "/rest/build-status/1.0/commits/cb3cf2e4d1517c83e720d2585b9402dbef71f992":
			_, err = io.WriteString(w, `{
						"size": 3,
						"limit": 100,
						"isLastPage": true,
						"values": [
							{
								"state": "SUCCESSFUL",
								"name": "DOCKER-BUILD #1"
							},
							{
								"state": "FAILED",
								"name": "e2e"
							},
							{
								"state": "INPROGRESS",
								"name": "docs"
							}
						],
						"start": 0
					}`)
		default:
			t.Fail()
		}
		if err != nil {
			t.Fail()
		}
	}
}

func TestListPullRequestNoAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		defaultHandler(t)(w, r)
	}))
	defer ts.Close()
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, nil, false)
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pullRequests))
	assert.Equal(t, 101, pullRequests[0].Number)
	assert.Equal(t, "feature-ABC-123", pullRequests[0].Branch)
	assert.Equal(t, "cb3cf2e4d1517c83e720d2585b9402dbef71f992", pullRequests[0].HeadSHA)
}

func TestListPullRequestPagination(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var err error
		switch r.RequestURI {
		case "/rest/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100":
			_, err = io.WriteString(w, `{
					"size": 2,
					"limit": 2,
					"isLastPage": false,
					"values": [
						{
							"id": 101,
							"fromRef": {
								"id": "refs/heads/feature-101",
								"displayId": "feature-101",
								"latestCommit": "ab3cf2e4d1517c83e720d2585b9402dbef71f992"
							}
						},
						{
							"id": 102,
							"fromRef": {
								"id": "refs/heads/feature-102",
								"displayId": "feature-102",
								"latestCommit": "bb3cf2e4d1517c83e720d2585b9402dbef71f992"
							}
						}
					],
					"nextPageStart": 200
				}`)
		case "/rest/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100&start=200":
			_, err = io.WriteString(w, `{
				"size": 1,
				"limit": 2,
				"isLastPage": true,
				"values": [
					{
						"id": 200,
						"fromRef": {
							"id": "refs/heads/feature-200",
							"displayId": "feature-200",
							"latestCommit": "cb3cf2e4d1517c83e720d2585b9402dbef71f992"
						}
					}
				],
				"start": 200
			}`)
		default:
			t.Fail()
		}
		if err != nil {
			t.Fail()
		}
	}))
	defer ts.Close()
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, nil, false)
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 3, len(pullRequests))
	assert.Equal(t, PullRequest{
		Number:  101,
		Branch:  "feature-101",
		HeadSHA: "ab3cf2e4d1517c83e720d2585b9402dbef71f992",
	}, *pullRequests[0])
	assert.Equal(t, PullRequest{
		Number:  102,
		Branch:  "feature-102",
		HeadSHA: "bb3cf2e4d1517c83e720d2585b9402dbef71f992",
	}, *pullRequests[1])
	assert.Equal(t, PullRequest{
		Number:  200,
		Branch:  "feature-200",
		HeadSHA: "cb3cf2e4d1517c83e720d2585b9402dbef71f992",
	}, *pullRequests[2])
}

func TestListPullRequestBasicAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// base64(user:password)
		assert.Equal(t, "Basic dXNlcjpwYXNzd29yZA==", r.Header.Get("Authorization"))
		assert.Equal(t, "no-check", r.Header.Get("X-Atlassian-Token"))
		defaultHandler(t)(w, r)
	}))
	defer ts.Close()
	svc, err := NewBitbucketServiceBasicAuth(context.TODO(), "user", "password", ts.URL, "PROJECT", "REPO", nil, nil, false)
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pullRequests))
	assert.Equal(t, 101, pullRequests[0].Number)
	assert.Equal(t, "feature-ABC-123", pullRequests[0].Branch)
	assert.Equal(t, "cb3cf2e4d1517c83e720d2585b9402dbef71f992", pullRequests[0].HeadSHA)
}

func TestListResponseError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()
	svc, _ := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, nil, false)
	_, err := svc.List(context.TODO())
	assert.NotNil(t, err, err)
}

func TestListResponseMalformed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.RequestURI {
		case "/rest/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100":
			_, err := io.WriteString(w, `{
					"size": 1,
					"limit": 100,
					"isLastPage": true,
					"values": { "id": 101 },
					"start": 0
				}`)
			if err != nil {
				t.Fail()
			}
		default:
			t.Fail()
		}
	}))
	defer ts.Close()
	svc, _ := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, nil, false)
	_, err := svc.List(context.TODO())
	assert.NotNil(t, err, err)
}

func TestListResponseEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.RequestURI {
		case "/rest/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100":
			_, err := io.WriteString(w, `{
					"size": 0,
					"limit": 100,
					"isLastPage": true,
					"values": [],
					"start": 0
				}`)
			if err != nil {
				t.Fail()
			}
		default:
			t.Fail()
		}
	}))
	defer ts.Close()
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, nil, false)
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Empty(t, pullRequests)
}

func TestListPullRequestBranchMatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var err error
		switch r.RequestURI {
		case "/rest/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100":
			_, err = io.WriteString(w, `{
					"size": 2,
					"limit": 2,
					"isLastPage": false,
					"values": [
						{
							"id": 101,
							"fromRef": {
								"id": "refs/heads/feature-101",
								"displayId": "feature-101",
								"latestCommit": "ab3cf2e4d1517c83e720d2585b9402dbef71f992"
							}
						},
						{
							"id": 102,
							"fromRef": {
								"id": "refs/heads/feature-102",
								"displayId": "feature-102",
								"latestCommit": "bb3cf2e4d1517c83e720d2585b9402dbef71f992"
							}
						}
					],
					"nextPageStart": 200
				}`)
		case "/rest/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100&start=200":
			_, err = io.WriteString(w, `{
				"size": 1,
				"limit": 2,
				"isLastPage": true,
				"values": [
					{
						"id": 200,
						"fromRef": {
							"id": "refs/heads/feature-200",
							"displayId": "feature-200",
							"latestCommit": "cb3cf2e4d1517c83e720d2585b9402dbef71f992"
						}
					}
				],
				"start": 200
			}`)
		default:
			t.Fail()
		}
		if err != nil {
			t.Fail()
		}
	}))
	defer ts.Close()
	regexp := `feature-1[\d]{2}`
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", &regexp, nil, false)
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 2, len(pullRequests))
	assert.Equal(t, PullRequest{
		Number:  101,
		Branch:  "feature-101",
		HeadSHA: "ab3cf2e4d1517c83e720d2585b9402dbef71f992",
	}, *pullRequests[0])
	assert.Equal(t, PullRequest{
		Number:  102,
		Branch:  "feature-102",
		HeadSHA: "bb3cf2e4d1517c83e720d2585b9402dbef71f992",
	}, *pullRequests[1])

	regexp = `.*2$`
	svc, err = NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", &regexp, nil, false)
	assert.Nil(t, err)
	pullRequests, err = svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pullRequests))
	assert.Equal(t, PullRequest{
		Number:  102,
		Branch:  "feature-102",
		HeadSHA: "bb3cf2e4d1517c83e720d2585b9402dbef71f992",
	}, *pullRequests[0])

	regexp = `[\d{2}`
	_, err = NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", &regexp, nil, false)
	assert.NotNil(t, err)
}

func TestListPullRequestAllBuildsGreen(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		switch r.RequestURI {
		case "/rest/api/1.0/projects/PROJECT/repos/REPOGREEN/pull-requests?limit=100":
			_, err := io.WriteString(w, `{
					"size": 1,
					"limit": 100,
					"isLastPage": true,
					"values": [
						{
							"id": 200,
							"fromRef": {
								"id": "refs/heads/feature-green",
								"displayId": "feature-green",
								"latestCommit": "de3cf2e4d1517c83e720d2585b9402dbef71f992"
							}
						}
					],
					"start": 0
				}`)
			if err != nil {
				t.Fail()
			}
			return
		case "/rest/build-status/1.0/commits/de3cf2e4d1517c83e720d2585b9402dbef71f992":
			_, err := io.WriteString(w, `{
						"size": 3,
						"limit": 100,
						"isLastPage": true,
						"values": [
							{
								"state": "SUCCESSFUL",
								"name": "DOCKER-BUILD #1"
							},
							{
								"state": "SUCCESSFUL",
								"name": "e2e"
							},
							{
								"state": "SUCCESSFUL",
								"name": "docs"
							}
						],
						"start": 0
					}`)
			if err != nil {
				t.Fail()
			}
			return
		}
		defaultHandler(t)(w, r)
	}))
	defer ts.Close()
	requestAllBuildsGreen := []string{}
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, requestAllBuildsGreen, false)
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 0, len(pullRequests))

	svc, err = NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPOGREEN", nil, requestAllBuildsGreen, false)
	assert.Nil(t, err)
	pullRequests, err = svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pullRequests))
	assert.Equal(t, 200, pullRequests[0].Number)
	assert.Equal(t, "feature-green", pullRequests[0].Branch)
	assert.Equal(t, "de3cf2e4d1517c83e720d2585b9402dbef71f992", pullRequests[0].HeadSHA)
}

func TestListPullRequestListedBuildsGreen(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(defaultHandler(t)))
	defer ts.Close()
	requestListedBuildsGreen := []string{`DOCKER-BUILD #\d+`}
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, requestListedBuildsGreen, false)
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pullRequests))
	assert.Equal(t, 101, pullRequests[0].Number)
	assert.Equal(t, "feature-ABC-123", pullRequests[0].Branch)
	assert.Equal(t, "cb3cf2e4d1517c83e720d2585b9402dbef71f992", pullRequests[0].HeadSHA)

	requestListedBuildsGreen = []string{`DOCKER-BUILD #\d+`, `docs`}
	svc, err = NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, requestListedBuildsGreen, false)
	assert.Nil(t, err)
	pullRequests, err = svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 0, len(pullRequests))
}

func TestListPullRequestCombineBranchMatchWithBuilds(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(defaultHandler(t)))
	defer ts.Close()
	requestListedBuildsGreen := []string{`DOCKER-BUILD #\d+`}
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, requestListedBuildsGreen, false)
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pullRequests))
	assert.Equal(t, 101, pullRequests[0].Number)
	assert.Equal(t, "feature-ABC-123", pullRequests[0].Branch)
	assert.Equal(t, "cb3cf2e4d1517c83e720d2585b9402dbef71f992", pullRequests[0].HeadSHA)

	branchMatch := `doesnotmatchbranch`
	svc, err = NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", &branchMatch, requestListedBuildsGreen, false)
	assert.Nil(t, err)
	pullRequests, err = svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 0, len(pullRequests))
}

func TestListPullRequestFindLatestGreenBuild(t *testing.T) {
	commitApiCalls := []int{0, 0}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.RequestURI {
		case "/rest/api/1.0/projects/PROJECT/repos/REPO/pull-requests/101/commits?limit=100":
			_, err := io.WriteString(w, `{
					"size": 3,
					"limit": 100,
					"isLastPage": true,
					"values": [
						{
							"id": "cb3cf2e4d1517c83e720d2585b9402dbef71f992"
						},
						{
							"id": "de3cf2e4d1517c83e720d2585b9402dbef71f992"
						},
						{
							"id": "ee3cf2e4d1517c83e720d2585b9402dbef71f992"
						}
					],
					"start": 0
				}`)
			if err != nil {
				t.Fail()
			}
			return
		case "/rest/build-status/1.0/commits/de3cf2e4d1517c83e720d2585b9402dbef71f992":
			commitApiCalls[0] += 1
			_, err := io.WriteString(w, `{
						"size": 1,
						"limit": 100,
						"isLastPage": true,
						"values": [
							{
								"state": "FAILED",
								"name": "e2e"
							}
						],
						"start": 0
					}`)
			if err != nil {
				t.Fail()
			}
			return
		case "/rest/build-status/1.0/commits/ee3cf2e4d1517c83e720d2585b9402dbef71f992":
			commitApiCalls[1] += 1
			_, err := io.WriteString(w, `{
						"size": 1,
						"limit": 100,
						"isLastPage": true,
						"values": [
							{
								"state": "SUCCESSFUL",
								"name": "e2e"
							}
						],
						"start": 0
					}`)
			if err != nil {
				t.Fail()
			}
			return
		}
		defaultHandler(t)(w, r)
	}))
	defer ts.Close()
	requestListedBuildsGreen := []string{`e\de`}
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, requestListedBuildsGreen, true)
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pullRequests))
	assert.Equal(t, 101, pullRequests[0].Number)
	assert.Equal(t, "feature-ABC-123", pullRequests[0].Branch)
	assert.Equal(t, "ee3cf2e4d1517c83e720d2585b9402dbef71f992", pullRequests[0].HeadSHA)

	requestListedBuildsGreen = []string{`docs`}
	svc, err = NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO", nil, requestListedBuildsGreen, true)
	assert.Nil(t, err)
	pullRequests, err = svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 0, len(pullRequests))
	assert.Equal(t, []int{2, 2}, commitApiCalls, "all commits should be traversed")
}
