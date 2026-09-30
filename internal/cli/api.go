package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func (a *App) apiCmd() *cobra.Command {
	var queries, fields, rawFields []string
	var input string
	var paginate, autoVersion, forceVersion, dryRun, confirmDelete bool
	cmd := &cobra.Command{
		Use:   "api METHOD PATH",
		Short: "Call any Taiga API v1 endpoint (PATH is relative to /api/v1/)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			method, path := strings.ToUpper(args[0]), strings.TrimPrefix(args[1], "/")
			usage := func(code, cause string) error {
				return &output.Error{Code: code, Source: "flag", Cause: cause, Exit: output.ExitUsage}
			}
			if method == "DELETE" && !confirmDelete {
				return usage("delete_not_confirmed", "DELETE requires --confirm-delete")
			}
			if paginate && method != "GET" {
				return usage("usage", "--paginate only applies to GET")
			}
			if autoVersion && method != "PATCH" && method != "PUT" {
				return usage("usage", "--auto-version only applies to PATCH and PUT")
			}
			q := url.Values{}
			for _, kv := range queries {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return usage("usage", "--query expects key=value: "+kv)
				}
				q.Add(k, v)
			}
			body, err := a.buildBody(input, fields, rawFields)
			if err != nil {
				return err
			}
			rc, err := a.runContext()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			c, err := a.client(ctx, rc)
			if err != nil {
				return err
			}
			if dryRun {
				if autoVersion {
					if body, _, err = c.PrepareVersioned(ctx, path, body); err != nil {
						return taiga.ToOutput(err)
					}
				}
				target := c.BaseURL() + "/api/v1/" + path
				return output.WriteJSON(a.Out, map[string]any{"dry_run": true, "method": method, "url": target, "query": q, "body": body})
			}
			var resp *taiga.Response
			switch {
			case paginate:
				items, err := c.GetAll(ctx, path, q)
				if err != nil {
					return taiga.ToOutput(err)
				}
				return output.WriteJSON(a.Out, items)
			case autoVersion:
				resp, err = c.WriteVersioned(ctx, method, path, body, forceVersion)
			default:
				var reqBody any
				if body != nil {
					reqBody = body
				}
				resp, err = c.Do(ctx, taiga.Request{Method: method, Path: path, Query: q, Body: reqBody})
			}
			if err != nil {
				return taiga.ToOutput(err)
			}
			if len(bytes.TrimSpace(resp.Body)) == 0 {
				return nil
			}
			var pretty any
			if json.Unmarshal(resp.Body, &pretty) != nil {
				_, err = a.Out.Write(resp.Body)
				return err
			}
			return output.WriteJSON(a.Out, pretty)
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&queries, "query", nil, "query parameter key=value (repeatable)")
	f.StringArrayVarP(&fields, "field", "F", nil, "body field key=value; value parsed as JSON when valid (repeatable)")
	f.StringArrayVarP(&rawFields, "raw-field", "f", nil, "body field key=value; value always a string (repeatable)")
	f.StringVar(&input, "input", "", "read the JSON body from a file, or - for stdin")
	f.BoolVar(&paginate, "paginate", false, "GET every page and print one JSON array")
	f.BoolVar(&autoVersion, "auto-version", false, "read the current version and send it (PATCH/PUT)")
	f.BoolVar(&forceVersion, "force-version", false, "with --auto-version, retry even if the same fields changed concurrently")
	f.BoolVar(&dryRun, "dry-run", false, "print the request instead of sending it")
	f.BoolVar(&confirmDelete, "confirm-delete", false, "required to send DELETE")
	return cmd
}

func (a *App) buildBody(input string, fields, rawFields []string) (map[string]any, error) {
	var body map[string]any
	if input != "" {
		var r io.Reader
		if input == "-" {
			r = a.In
		} else {
			f, err := os.Open(input)
			if err != nil {
				return nil, &output.Error{Code: "usage", Source: "flag", Cause: err.Error(), Exit: output.ExitUsage}
			}
			defer func() { _ = f.Close() }()
			r = f
		}
		dec := json.NewDecoder(r)
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			return nil, &output.Error{Code: "usage", Source: "flag", Cause: "--input must be a JSON object: " + err.Error(), Exit: output.ExitUsage}
		}
	}
	set := func(kv string, raw bool) error {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return &output.Error{Code: "usage", Source: "flag", Cause: "field expects key=value: " + kv, Exit: output.ExitUsage}
		}
		if body == nil {
			body = map[string]any{}
		}
		if !raw && json.Valid([]byte(v)) {
			dec := json.NewDecoder(strings.NewReader(v))
			dec.UseNumber()
			var parsed any
			if dec.Decode(&parsed) == nil {
				body[k] = parsed
				return nil
			}
		}
		body[k] = v
		return nil
	}
	for _, kv := range fields {
		if err := set(kv, false); err != nil {
			return nil, err
		}
	}
	for _, kv := range rawFields {
		if err := set(kv, true); err != nil {
			return nil, err
		}
	}
	return body, nil
}
