package observability

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	o11y "github.com/launchdarkly/ldcli/internal/observability"
)

const (
	sessionFlag   = "session"
	sortFieldFlag = "sort-field"
	ascendingFlag = "ascending"
	indexFlag     = "index"
	timestampFlag = "timestamp"
)

func addSessionFlag(cmd *cobra.Command) {
	cmd.Flags().String(sessionFlag, "", "The session secure ID")
	_ = cmd.MarkFlagRequired(sessionFlag)
	_ = cmd.Flags().SetAnnotation(sessionFlag, "required", []string{"true"})
}

func newSessionsCmd(d deps) *cobra.Command {
	return newGroupCmd("sessions", "Search sessions and inspect session replays",
		newSessionsQueryCmd(d),
		newTimelineEventsCmd(d),
		newFlagEvaluationsCmd(d),
		newEventChunksCmd(d),
		newEventChunkURLCmd(d),
		newTimestampEventChunkURLCmd(d),
	)
}

func newSessionsQueryCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"query",
		"Query sessions",
		"Query a project's recorded sessions over a time range, for example\n`has_errors=true AND browser_name=Chrome`.",
		func(cmd *cobra.Command) {
			addDateRangeFlags(cmd)
			cmd.Flags().String(queryFlag, "", "Search query, for example 'has_errors=true identifier=user@example.com'")
			cmd.Flags().Int(countFlag, 10, "Number of sessions per page (max 50)")
			cmd.Flags().Int(pageFlag, 1, "Page number, starting at 1")
			cmd.Flags().String(sortFieldFlag, "created_at", "Field to sort by")
			cmd.Flags().Bool(ascendingFlag, false, "Sort ascending instead of descending")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			dr, err := dateRange(cmd)
			if err != nil {
				return nil, nil, err
			}
			query, _ := cmd.Flags().GetString(queryFlag)
			count, _ := cmd.Flags().GetInt(countFlag)
			page, _ := cmd.Flags().GetInt(pageFlag)
			sortField, _ := cmd.Flags().GetString(sortFieldFlag)
			ascending, _ := cmd.Flags().GetBool(ascendingFlag)

			data, err := gql.Do(projectID, o11y.SessionsQuery, map[string]interface{}{
				"project_id": projectID,
				"params":     map[string]interface{}{"query": query, "date_range": dr},
				"count":      clamp(count, 50),
				"sort_field": sortField,
				"sort_desc":  !ascending,
				"page":       clamp(page, 1<<30),
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Sessions struct {
					Sessions []struct {
						SecureID    string  `json:"secure_id"`
						CreatedAt   string  `json:"created_at"`
						Identifier  string  `json:"identifier"`
						ActiveLen   float64 `json:"active_length"`
						HasErrors   *bool   `json:"has_errors"`
						BrowserName string  `json:"browser_name"`
						OSName      string  `json:"os_name"`
						City        string  `json:"city"`
						Country     string  `json:"country"`
					} `json:"sessions"`
					TotalCount int `json:"totalCount"`
				} `json:"sessions"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				sessions := res.Sessions.Sessions
				if len(sessions) == 0 {
					return emptyHint("sessions")
				}
				rows := make([][]string, 0, len(sessions))
				for _, s := range sessions {
					errs := ""
					if s.HasErrors != nil && *s.HasErrors {
						errs = "yes"
					}
					location := strings.Trim(s.City+", "+s.Country, ", ")
					rows = append(rows, []string{
						s.SecureID, s.CreatedAt, s.Identifier, formatMillis(s.ActiveLen),
						errs, strings.TrimSpace(s.BrowserName + " / " + s.OSName), location,
					})
				}
				return table([]string{"SECURE ID", "CREATED", "IDENTIFIER", "ACTIVE", "ERRORS", "BROWSER / OS", "LOCATION"}, rows) +
					fmt.Sprintf("\n\nShowing %d of %d sessions", len(sessions), res.Sessions.TotalCount)
			}, nil
		},
	)
}

// formatMillis renders a session length, which the backend reports in milliseconds.
func formatMillis(ms float64) string {
	if ms <= 0 {
		return ""
	}
	s := int64(ms / 1000)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
}

func newTimelineEventsCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"timeline-events",
		"List a session's timeline events",
		"List the timeline indicator events (clicks, navigations, errors, custom events, ...) recorded in a session.",
		addSessionFlag,
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			session, _ := cmd.Flags().GetString(sessionFlag)
			data, err := gql.Do(projectID, o11y.TimelineIndicatorEventsQuery, map[string]interface{}{
				"session_secure_id": session,
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Events []struct {
					Timestamp float64     `json:"timestamp"`
					Type      interface{} `json:"type"`
					Data      interface{} `json:"data"`
				} `json:"timeline_indicator_events"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				if len(res.Events) == 0 {
					return "No timeline events found."
				}
				rows := make([][]string, 0, len(res.Events))
				for _, e := range res.Events {
					rows = append(rows, []string{str(e.Timestamp), str(e.Type), truncate(str(e.Data), 100)})
				}
				return table([]string{"TIMESTAMP", "TYPE", "DATA"}, rows)
			}, nil
		},
	)
}

func newFlagEvaluationsCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"flag-evaluations",
		"List flag evaluations in a session",
		"List the feature flag evaluations recorded during a session.",
		func(cmd *cobra.Command) {
			addSessionFlag(cmd)
			addDateRangeFlags(cmd)
			cmd.Flags().String(queryFlag, "", "Additional search query to narrow the evaluations")
			cmd.Flags().Int(limitFlag, 50, "Number of evaluations to return (max 100)")
			addDirectionFlag(cmd)
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			session, _ := cmd.Flags().GetString(sessionFlag)
			dr, err := dateRange(cmd)
			if err != nil {
				return nil, nil, err
			}
			dir, err := direction(cmd)
			if err != nil {
				return nil, nil, err
			}
			limit, _ := cmd.Flags().GetInt(limitFlag)
			query := "events.name=feature_flag AND secure_session_id=" + session
			if extra, _ := cmd.Flags().GetString(queryFlag); strings.TrimSpace(extra) != "" {
				query += " AND " + strings.TrimSpace(extra)
			}

			data, err := gql.Do(projectID, o11y.FlagEvaluationsQuery, map[string]interface{}{
				"project_id": projectID,
				"params":     map[string]interface{}{"query": query, "date_range": dr},
				"direction":  dir,
				"limit":      clamp(limit, 100),
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Traces struct {
					Edges []struct {
						Node struct {
							Events []struct {
								Timestamp  string                 `json:"timestamp"`
								Name       string                 `json:"name"`
								Attributes map[string]interface{} `json:"attributes"`
							} `json:"events"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"traces"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				rows := [][]string{}
				for _, e := range res.Traces.Edges {
					for _, ev := range e.Node.Events {
						if ev.Name != "feature_flag" {
							continue
						}
						rows = append(rows, []string{
							ev.Timestamp,
							str(lookupAttr(ev.Attributes, "feature_flag.key")),
							truncate(str(lookupAttr(ev.Attributes, "feature_flag.result.value")), 60),
							str(lookupAttr(ev.Attributes, "feature_flag.context.key")),
						})
					}
				}
				if len(rows) == 0 {
					return "No flag evaluations found for this session and time range."
				}
				return table([]string{"TIMESTAMP", "FLAG", "VALUE", "CONTEXT"}, rows)
			}, nil
		},
	)
}

func newEventChunksCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"event-chunks",
		"List a session's replay event chunks",
		"List the chunks a session's replay events are stored in. Use `event-chunk-url` to download one.",
		addSessionFlag,
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			session, _ := cmd.Flags().GetString(sessionFlag)
			data, err := gql.Do(projectID, o11y.EventChunksQuery, map[string]interface{}{"secure_id": session})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Chunks []struct {
					ChunkIndex int     `json:"chunk_index"`
					Timestamp  float64 `json:"timestamp"`
				} `json:"event_chunks"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				if len(res.Chunks) == 0 {
					return "No event chunks found. Unchunked sessions store their replay as a single payload."
				}
				rows := make([][]string, 0, len(res.Chunks))
				for _, c := range res.Chunks {
					rows = append(rows, []string{fmt.Sprint(c.ChunkIndex), str(c.Timestamp)})
				}
				return table([]string{"INDEX", "TIMESTAMP"}, rows)
			}, nil
		},
	)
}

const chunkURLNote = "The URL is short-lived and the payload is Brotli-compressed rrweb event JSON."

func newEventChunkURLCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"event-chunk-url",
		"Get a download URL for a replay event chunk",
		"Get a pre-signed download URL for one of a session's replay event chunks. "+chunkURLNote,
		func(cmd *cobra.Command) {
			addSessionFlag(cmd)
			cmd.Flags().Int(indexFlag, 0, "The chunk index, from `event-chunks`")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			session, _ := cmd.Flags().GetString(sessionFlag)
			index, _ := cmd.Flags().GetInt(indexFlag)
			data, err := gql.Do(projectID, o11y.EventChunkURLQuery, map[string]interface{}{
				"secure_id": session,
				"index":     index,
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				URL string `json:"event_chunk_url"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string { return res.URL }, nil
		},
	)
}

func newTimestampEventChunkURLCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"timestamp-event-chunk-url",
		"Get a download URL for the replay chunk containing a timestamp",
		"Get a pre-signed download URL for the replay event chunk that contains a given point in a session. "+chunkURLNote,
		func(cmd *cobra.Command) {
			addSessionFlag(cmd)
			cmd.Flags().String(timestampFlag, "", "The point in the session, as an RFC 3339 timestamp")
			_ = cmd.MarkFlagRequired(timestampFlag)
			_ = cmd.Flags().SetAnnotation(timestampFlag, "required", []string{"true"})
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			session, _ := cmd.Flags().GetString(sessionFlag)
			ts, _ := cmd.Flags().GetString(timestampFlag)
			data, err := gql.Do(projectID, o11y.TimestampEventChunkURLQuery, map[string]interface{}{
				"secure_id": session,
				"timestamp": ts,
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				URL string `json:"timestamp_event_chunk_url"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string { return res.URL }, nil
		},
	)
}

// lookupAttr reads a dotted attribute key from either a flat or a nested attribute map.
func lookupAttr(attrs map[string]interface{}, key string) interface{} {
	if v, ok := attrs[key]; ok {
		return v
	}
	head, rest, found := strings.Cut(key, ".")
	if !found {
		return nil
	}
	nested, ok := attrs[head].(map[string]interface{})
	if !ok {
		return nil
	}
	return lookupAttr(nested, rest)
}
