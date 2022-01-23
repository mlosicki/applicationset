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
		switch r.RequestURI {
		case "/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100":
			io.WriteString(w, `{
					"size": 1,
					"limit": 100,
					"isLastPage": true,
					"values": [
						{
							"id": 101,
							"fromRef": {
								"id": "refs/heads/feature-ABC-123",
								"latestCommit": "cb3cf2e4d1517c83e720d2585b9402dbef71f992"
							}
						}
					],
					"start": 0
				}`)
		default:
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
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO")
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pullRequests))
	assert.Equal(t, 101, pullRequests[0].Number)
	assert.Equal(t, "refs/heads/feature-ABC-123", pullRequests[0].Branch)
	assert.Equal(t, "cb3cf2e4d1517c83e720d2585b9402dbef71f992", pullRequests[0].HeadSHA)
}

func TestListPullRequestPagination(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.RequestURI {
		case "/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100":
			io.WriteString(w, `{
					"size": 2,
					"limit": 2,
					"isLastPage": false,
					"values": [
						{
							"id": 101,
							"fromRef": {
								"id": "refs/heads/feature-101",
								"latestCommit": "ab3cf2e4d1517c83e720d2585b9402dbef71f992"
							}
						},
						{
							"id": 102,
							"fromRef": {
								"id": "refs/heads/feature-102",
								"latestCommit": "bb3cf2e4d1517c83e720d2585b9402dbef71f992"
							}
						}
					],
					"nextPageStart": 200
				}`)
		case "/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100&start=200":
			io.WriteString(w, `{
				"size": 1,
				"limit": 2,
				"isLastPage": true,
				"values": [
					{
						"id": 200,
						"fromRef": {
							"id": "refs/heads/feature-200",
							"latestCommit": "cb3cf2e4d1517c83e720d2585b9402dbef71f992"
						}
					}
				],
				"start": 200
			}`)
		default:
			t.Fail()
		}
	}))
	defer ts.Close()
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO")
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 3, len(pullRequests))
	assert.Equal(t, PullRequest{
		Number:  101,
		Branch:  "refs/heads/feature-101",
		HeadSHA: "ab3cf2e4d1517c83e720d2585b9402dbef71f992",
	}, *pullRequests[0])
	assert.Equal(t, PullRequest{
		Number:  102,
		Branch:  "refs/heads/feature-102",
		HeadSHA: "bb3cf2e4d1517c83e720d2585b9402dbef71f992",
	}, *pullRequests[1])
	assert.Equal(t, PullRequest{
		Number:  200,
		Branch:  "refs/heads/feature-200",
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
	svc, err := NewBitbucketServiceBasicAuth(context.TODO(), "user", "password", ts.URL, "PROJECT", "REPO")
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Equal(t, 1, len(pullRequests))
	assert.Equal(t, 101, pullRequests[0].Number)
	assert.Equal(t, "refs/heads/feature-ABC-123", pullRequests[0].Branch)
	assert.Equal(t, "cb3cf2e4d1517c83e720d2585b9402dbef71f992", pullRequests[0].HeadSHA)
}

func TestListResponseError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()
	svc, _ := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO")
	_, err := svc.List(context.TODO())
	assert.NotNil(t, err, err)
}

func TestListResponseMalformed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.RequestURI {
		case "/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100":
			io.WriteString(w, `{
					"size": 1,
					"limit": 100,
					"isLastPage": true,
					"values": { "id": 101 },
					"start": 0
				}`)
		default:
			t.Fail()
		}
	}))
	defer ts.Close()
	svc, _ := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO")
	_, err := svc.List(context.TODO())
	assert.NotNil(t, err, err)
}

func TestListResponseEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.RequestURI {
		case "/api/1.0/projects/PROJECT/repos/REPO/pull-requests?limit=100":
			io.WriteString(w, `{
					"size": 0,
					"limit": 100,
					"isLastPage": true,
					"values": [],
					"start": 0
				}`)
		default:
			t.Fail()
		}
	}))
	defer ts.Close()
	svc, err := NewBitbucketServiceNoAuth(context.TODO(), ts.URL, "PROJECT", "REPO")
	assert.Nil(t, err)
	pullRequests, err := svc.List(context.TODO())
	assert.Nil(t, err)
	assert.Empty(t, pullRequests)
}
