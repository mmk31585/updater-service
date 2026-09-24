package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v9"
	"github.com/elastic/go-elasticsearch/v9/esapi"
	"github.com/mmk31585/updater-service/internal/operation"
)

const IndexName = "operations-v1"

type OperationDocument struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Service   string `json:"service"`
	NodeID    string `json:"node_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Client struct {
	es *elasticsearch.Client
}

func NewClient(address string) (*Client, error) {
	es, err := elasticsearch.New(
		elasticsearch.WithAddresses(address),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		es: es,
	}, nil
}

func (c *Client) EnsureIndex(
	ctx context.Context,
) error {
	mapping := `{
		"mappings": {
			"properties": {
				"id": {
					"type": "keyword"
				},
				"status": {
					"type": "keyword"
				},
				"service": {
					"type": "keyword"
				},
				"node_id": {
					"type": "keyword"
				},
				"created_at": {
					"type": "date"
				},
				"updated_at": {
					"type": "date"
				}
			}
		}
	}`

	req := esapi.IndicesCreateRequest{
		Index: IndexName,
		Body:  strings.NewReader(mapping),
	}

	res, err := req.Do(ctx, c.es)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	if !res.IsError() {
		return nil
	}

	if res.StatusCode == http.StatusBadRequest {
		return nil
	}

	return fmt.Errorf(
		"failed to create index: %s",
		res.String(),
	)
}

func toDocument(
	op operation.Operation,
) OperationDocument {
	return OperationDocument{
		ID:        op.ID,
		Status:    string(op.Status),
		Service:   op.Service,
		NodeID:    op.NodeID,
		CreatedAt: op.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: op.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (c *Client) IndexOperation(
	ctx context.Context,
	op operation.Operation,
) error {
	document := toDocument(op)

	body, err := json.Marshal(document)
	if err != nil {
		return err
	}

	req := esapi.IndexRequest{
		Index:      IndexName,
		DocumentID: op.ID,
		Body:       bytes.NewReader(body),
		Refresh:    "wait_for",
	}

	res, err := req.Do(ctx, c.es)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf(
			"elasticsearch index failed: %s",
			res.String(),
		)
	}

	return nil
}
