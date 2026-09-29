package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// partnerCarrier is the partner carrier that delivers the parcels going
// beyond the EU, as the service asks it about them: its MCP server (the
// shipment_status tool), which the parcel assistant asks, and its A2A
// agent, which the parcels agent asks.
type partnerCarrier struct {
	mcpURL   string // its MCP server (streamable HTTP)
	agentURL string // its A2A agent (JSON-RPC)
	http     *http.Client
}

// shipmentStatus asks the partner's MCP server where a parcel is: the
// structured result of its shipment_status tool.
func (p *partnerCarrier) shipmentStatus(ctx context.Context, reference string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := sdk.NewClient(&sdk.Implementation{Name: "parcels", Version: "1.4.0"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: p.mcpURL, HTTPClient: p.http, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		return nil, fmt.Errorf("the partner carrier's MCP server: %w", err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "shipment_status", Arguments: map[string]any{"reference": reference}})
	if err != nil {
		return nil, fmt.Errorf("the partner carrier's shipment_status: %w", err)
	}
	if res.IsError {
		var texts []string
		for _, c := range res.Content {
			if t, ok := c.(*sdk.TextContent); ok {
				texts = append(texts, t.Text)
			}
		}
		return nil, errors.New("the partner carrier's shipment_status: " + strings.Join(texts, " "))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return nil, err
	}
	var status map[string]any
	if err := json.Unmarshal(b, &status); err != nil || status == nil {
		return nil, errors.New("the partner carrier's shipment_status answered no structured status")
	}
	return status, nil
}

// ask asks the partner's A2A agent a question, and returns its answer: its
// reply, or its task's status message.
func (p *partnerCarrier) ask(ctx context.Context, question string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client, err := a2aclient.NewFromEndpoints(ctx, []*a2a.AgentInterface{a2a.NewAgentInterface(p.agentURL+"/a2a", a2a.TransportProtocolJSONRPC)})
	if err != nil {
		return "", err
	}
	defer func() { _ = client.Destroy() }()
	res, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(question))})
	if err != nil {
		return "", err
	}
	switch r := res.(type) {
	case *a2a.Message:
		return textOf(r), nil
	case *a2a.Task:
		if r.Status.State != a2a.TaskStateCompleted {
			return "", fmt.Errorf("the partner carrier's agent's task is %s", r.Status.State)
		}
		return textOf(r.Status.Message), nil
	}
	return "", errors.New("the partner carrier's agent answered nothing")
}
