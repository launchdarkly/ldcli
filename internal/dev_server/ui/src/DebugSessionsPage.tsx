import { useEffect, useState } from "react";
import { apiRoute } from "./util";
import { DebugSession, DebugSessionsPage as DebugSessionsPageType } from "./types";
import { Box, CopyToClipboard, Alert } from "@launchpad-ui/core";
import { Heading, Text, ProgressBar, Button } from "@launchpad-ui/components";
import { Icon } from "@launchpad-ui/icons";

const DebugSessionsPage = () => {
  const [debugSessions, setDebugSessions] = useState<DebugSession[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [totalCount, setTotalCount] = useState<number>(0);

  const fetchDebugSessions = async () => {
    try {
      setLoading(true);
      setError(null);
      
      const response = await fetch(apiRoute("/dev/debug-sessions?limit=100"));
      
      if (!response.ok) {
        throw new Error(`Failed to fetch debug sessions: ${response.status} ${response.statusText}`);
      }
      
      const data: DebugSessionsPageType = await response.json();
      setDebugSessions(data.sessions);
      setTotalCount(data.total_count);
    } catch (err) {
      setError(err instanceof Error ? err.message : "An unknown error occurred");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchDebugSessions();
  }, []);

  const formatDate = (dateString: string) => {
    try {
      const date = new Date(dateString);
      return date.toLocaleString();
    } catch {
      return dateString;
    }
  };

  if (loading) {
    return (
      <Box padding="2rem">
        <Heading>Debug Sessions</Heading>
        <Box marginTop="1rem">
          <ProgressBar isIndeterminate />
        </Box>
        <Box marginTop="1rem">
          <Text>Loading debug sessions...</Text>
        </Box>
      </Box>
    );
  }

  if (error) {
    return (
      <Box padding="2rem">
        <Heading>Debug Sessions</Heading>
        <Box marginTop="1rem">
          <Alert kind="error">
            <Text>Error: {error}</Text>
          </Alert>
        </Box>
        <Box marginTop="1rem">
          <Button onPress={fetchDebugSessions}>Retry</Button>
        </Box>
      </Box>
    );
  }

  return (
    <Box padding="2rem">
      <Box display="flex" justifyContent="space-between" alignItems="center" marginBottom="1rem">
        <Heading>Debug Sessions</Heading>
        <Text color="var(--lp-color-text-ui-secondary)">
          {totalCount} total session{totalCount !== 1 ? 's' : ''}
        </Text>
      </Box>

      {debugSessions.length === 0 ? (
        <Box
          padding="2rem"
          textAlign="center"
          backgroundColor="var(--lp-color-bg-ui-secondary)"
          borderRadius="4px"
        >
          <Icon name="data" size="large" />
          <Box marginTop="1rem">
            <Text>No debug sessions found</Text>
          </Box>
          <Box marginTop="0.5rem">
            <Text color="var(--lp-color-text-ui-secondary)">
              Debug sessions will appear here when events are captured
            </Text>
          </Box>
        </Box>
      ) : (
        <div
          style={{
            border: "1px solid var(--lp-color-border-ui-primary)",
            borderRadius: "4px",
            overflow: "hidden"
          }}
        >
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <thead>
              <tr style={{ backgroundColor: "var(--lp-color-bg-ui-secondary)" }}>
                <th style={{ 
                  padding: "0.75rem", 
                  textAlign: "left", 
                  borderBottom: "1px solid var(--lp-color-border-ui-primary)",
                  fontWeight: 600
                }}>
                  Session Key
                </th>
                <th style={{ 
                  padding: "0.75rem", 
                  textAlign: "left", 
                  borderBottom: "1px solid var(--lp-color-border-ui-primary)",
                  fontWeight: 600
                }}>
                  Created At
                </th>
                <th style={{ 
                  padding: "0.75rem", 
                  textAlign: "right", 
                  borderBottom: "1px solid var(--lp-color-border-ui-primary)",
                  fontWeight: 600
                }}>
                  Event Count
                </th>
                <th style={{ 
                  padding: "0.75rem", 
                  textAlign: "center", 
                  borderBottom: "1px solid var(--lp-color-border-ui-primary)",
                  fontWeight: 600,
                  width: "100px"
                }}>
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {debugSessions.map((session, index) => (
                <tr 
                  key={session.key}
                  style={{ 
                    borderBottom: index < debugSessions.length - 1 ? "1px solid var(--lp-color-border-ui-primary)" : "none"
                  }}
                >
                  <td style={{ padding: "0.75rem" }}>
                    <Text style={{ fontFamily: "monospace" }}>
                      {session.key}
                    </Text>
                  </td>
                  <td style={{ padding: "0.75rem" }}>
                    <Text>
                      {formatDate(session.written_at)}
                    </Text>
                  </td>
                  <td style={{ padding: "0.75rem", textAlign: "right" }}>
                    <Text>
                      {session.event_count.toLocaleString()}
                    </Text>
                  </td>
                  <td style={{ padding: "0.75rem", textAlign: "center" }}>
                    <CopyToClipboard text={session.key}>
                      <button
                        style={{
                          background: "none",
                          border: "1px solid var(--lp-color-border-ui-primary)",
                          borderRadius: "4px",
                          padding: "0.25rem 0.5rem",
                          cursor: "pointer",
                          display: "flex",
                          alignItems: "center",
                          gap: "0.25rem"
                        }}
                        title="Copy session key"
                      >
                        <Icon name="link" size="small" />
                      </button>
                    </CopyToClipboard>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Box>
  );
};

export default DebugSessionsPage;
