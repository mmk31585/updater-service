package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch/v9/esapi"
)

type SearchFilter struct {
	Status  string
	Service string
	NodeID  string
	Limit   int
	Offset  int
}

type SearchResult struct {
	Items []OperationDocument `json:"items"`
	Total int64               `json:"total"`
}

func (c *Client) SearchOperations(
	ctx context.Context,
	filter SearchFilter,
) (SearchResult, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}

	if filter.Limit > 100 {
		filter.Limit = 100
	}

	var filters []map[string]any

	if filter.Status != "" {
		filters = append(
			filters,
			map[string]any{
				"term": map[string]any{
					"status": filter.Status,
				},
			},
		)
	}

	if filter.Service != "" {
		filters = append(
			filters,
			map[string]any{
				"term": map[string]any{
					"service": filter.Service,
				},
			},
		)
	}

	if filter.NodeID != "" {
		filters = append(
			filters,
			map[string]any{
				"term": map[string]any{
					"node_id": filter.NodeID,
				},
			},
		)
	}

	query := map[string]any{
		"bool": map[string]any{
			"filter": filters,
		},
	}

	body := map[string]any{
		"query": query,
		"sort": []any{
			map[string]any{
				"updated_at": map[string]any{
					"order": "desc",
				},
			},
		},
		"from": filter.Offset,
		"size": filter.Limit,
	}

	requestBody, err := json.Marshal(body)
	if err != nil {
		return SearchResult{}, err
	}

	req := esapi.SearchRequest{
		Index: []string{IndexName},
		Body:  bytes.NewReader(requestBody),
	}

	res, err := req.Do(ctx, c.es)
	if err != nil {
		return SearchResult{}, err
	}

	defer res.Body.Close()

	if res.IsError() {
		return SearchResult{}, fmt.Errorf(
			"elasticsearch search failed: %s",
			res.String(),
		)
	}

	var response struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`

			Hits []struct {
				Source OperationDocument `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}

	if err := json.NewDecoder(
		res.Body,
	).Decode(&response); err != nil {
		return SearchResult{}, err
	}

	items := make(
		[]OperationDocument,
		0,
		len(response.Hits.Hits),
	)

	for _, hit := range response.Hits.Hits {
		items = append(
			items,
			hit.Source,
		)
	}

	return SearchResult{
		Items: items,
		Total: response.Hits.Total.Value,
	}, nil
}
