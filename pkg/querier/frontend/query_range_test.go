package frontend

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"strconv"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/ingester/client"
)

const (
	query        = "/api/v1/query_range?end=1536716898&query=sum%28container_memory_rss%29+by+%28namespace%29&start=1536673680&step=120"
	responseBody = `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1536673680,"137"],[1536673780,"137"]]}]}}`
)

var parsedResponse = &APIResponse{
	Status: "success",
	Data: QueryRangeResponse{
		ResultType: model.ValMatrix.String(),
		Result: []SampleStream{
			SampleStream{
				Samples: []client.Sample{
					{1536673680000, 137},
					{1536673780000, 137},
				},
			},
		},
	},
}

var parsedRequest = &QueryRangeRequest{
	Path:  "/api/v1/query_range",
	Start: 1536673680 * 1e3,
	End:   1536716898 * 1e3,
	Step:  120 * 1e3,
	Query: "sum(container_memory_rss) by (namespace)",
}

func TestQueryRangeRequest(t *testing.T) {
	for i, tc := range []struct {
		url         string
		expected    *QueryRangeRequest
		expectedErr error
	}{
		{
			url:      query,
			expected: parsedRequest,
		},
		{
			url:         "api/v1/query_range?start=foo",
			expectedErr: httpgrpc.Errorf(http.StatusBadRequest, "cannot parse \"foo\" to a valid timestamp"),
		},
		{
			url:         "api/v1/query_range?start=123&end=bar",
			expectedErr: httpgrpc.Errorf(http.StatusBadRequest, "cannot parse \"bar\" to a valid timestamp"),
		},
		{
			url:         "api/v1/query_range?start=123&end=0",
			expectedErr: errEndBeforeStart,
		},
		{
			url:         "api/v1/query_range?start=123&end=456&step=baz",
			expectedErr: httpgrpc.Errorf(http.StatusBadRequest, "cannot parse \"baz\" to a valid duration"),
		},
		{
			url:         "api/v1/query_range?start=123&end=456&step=-1",
			expectedErr: errNegativeStep,
		},
		{
			url:         "api/v1/query_range?start=0&end=11001&step=1",
			expectedErr: errStepTooSmall,
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			r, err := http.NewRequest("GET", tc.url, nil)
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "1")
			r = r.WithContext(ctx)

			req, err := parseQueryRangeRequest(r)
			if err != nil {
				require.EqualValues(t, tc.expectedErr, err)
				return
			}
			require.EqualValues(t, tc.expected, req)

			rdash, err := req.toHTTPRequest(context.Background())
			require.NoError(t, err)
			require.EqualValues(t, tc.url, rdash.RequestURI)
		})
	}
}

func TestQueryRangeResponse(t *testing.T) {
	for i, tc := range []struct {
		body     string
		expected *APIResponse
	}{
		{
			body:     responseBody,
			expected: parsedResponse,
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			response := &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       ioutil.NopCloser(bytes.NewBuffer([]byte(tc.body))),
			}
			resp, err := parseQueryRangeResponse(response)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, resp)

			// Reset response, as the above call will have consumed the body reader.
			response = &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       ioutil.NopCloser(bytes.NewBuffer([]byte(tc.body))),
			}
			resp2, err := resp.toHTTPResponse()
			require.NoError(t, err)
			assert.Equal(t, response, resp2)
		})
	}
}

func TestMergeAPIResponses(t *testing.T) {
	for i, tc := range []struct {
		input    []*APIResponse
		expected *APIResponse
	}{
		// No responses shouldn't panic.
		{
			input: []*APIResponse{},
			expected: &APIResponse{
				Status: statusSuccess,
			},
		},

		// A single empty response shouldn't panic.
		{
			input: []*APIResponse{
				{
					Data: QueryRangeResponse{
						ResultType: model.ValMatrix.String(),
					},
				},
			},
			expected: &APIResponse{
				Status: statusSuccess,
				Data: QueryRangeResponse{
					ResultType: model.ValMatrix.String(),
				},
			},
		},

		// Multiple empty responses shouldn't panic.
		{
			input: []*APIResponse{
				{
					Data: QueryRangeResponse{
						ResultType: model.ValMatrix.String(),
					},
				},
				{
					Data: QueryRangeResponse{
						ResultType: model.ValMatrix.String(),
					},
				},
			},
			expected: &APIResponse{
				Status: statusSuccess,
				Data: QueryRangeResponse{
					ResultType: model.ValMatrix.String(),
				},
			},
		},

		// Basic merging of two responses.
		{
			input: []*APIResponse{
				{
					Data: QueryRangeResponse{
						ResultType: matrix,
						Result: []SampleStream{
							{
								Samples: []client.Sample{
									{0, 0},
									{1, 1},
								},
							},
						},
					},
				},
				{
					Data: QueryRangeResponse{
						ResultType: matrix,
						Result: []SampleStream{
							{
								Samples: []client.Sample{
									{2, 2},
									{3, 3},
								},
							},
						},
					},
				},
			},
			expected: &APIResponse{
				Status: statusSuccess,
				Data: QueryRangeResponse{
					ResultType: matrix,
					Result: []SampleStream{
						{
							Samples: []client.Sample{
								{0, 0},
								{1, 1},
								{2, 2},
								{3, 3},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			output, err := mergeAPIResponses(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.expected, output)
		})
	}
}
