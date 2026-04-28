package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
)

var ErrNotCancellable = errors.New("run type is not cancellable")

func CanCancel(id string) error {
	kind, ok := RunKindFromID(id)
	if !ok {
		return errors.New("unknown run id prefix")
	}
	if kind == KindParse {
		return errors.New("parse runs cannot be cancelled")
	}
	if kind == KindEdit {
		return errors.New("edit runs cannot be cancelled")
	}
	return nil
}

func (c *Client) cancelRun(ctx context.Context, path string) error {
	resp, err := c.do(ctx, http.MethodPost, path, bytes.NewReader([]byte("{}")), "application/json")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) CancelExtractRun(ctx context.Context, id string) error {
	return c.cancelRun(ctx, "/extract_runs/"+id+"/cancel")
}

func (c *Client) CancelClassifyRun(ctx context.Context, id string) error {
	return c.cancelRun(ctx, "/classify_runs/"+id+"/cancel")
}

func (c *Client) CancelSplitRun(ctx context.Context, id string) error {
	return c.cancelRun(ctx, "/split_runs/"+id+"/cancel")
}

func (c *Client) CancelRun(ctx context.Context, id string) error {
	kind, ok := RunKindFromID(id)
	if !ok {
		return errors.New("unknown run id prefix")
	}
	switch kind {
	case KindExtract:
		return c.CancelExtractRun(ctx, id)
	case KindClassify:
		return c.CancelClassifyRun(ctx, id)
	case KindSplit:
		return c.CancelSplitRun(ctx, id)
	case KindWorkflow:
		return c.CancelWorkflowRun(ctx, id)
	case KindParse:
		return ErrNotCancellable
	}
	return errors.New("unsupported run kind")
}

func (c *Client) deleteRunPath(ctx context.Context, path string) error {
	resp, err := c.do(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) DeleteRun(ctx context.Context, id string) error {
	kind, ok := RunKindFromID(id)
	if !ok {
		return errors.New("unknown run id prefix")
	}
	var prefix string
	switch kind {
	case KindExtract:
		prefix = "/extract_runs/"
	case KindParse:
		prefix = "/parse_runs/"
	case KindClassify:
		prefix = "/classify_runs/"
	case KindSplit:
		prefix = "/split_runs/"
	case KindEdit:
		prefix = "/edit_runs/"
	case KindWorkflow:
		prefix = "/workflow_runs/"
	default:
		return fmt.Errorf("unsupported run kind %s", kind)
	}
	return c.deleteRunPath(ctx, prefix+id)
}
