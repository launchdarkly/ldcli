import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { apiRoute } from "./util";
import { DebugSession, DebugSessionsPage as DebugSessionsPageType } from "./types";
import { Box, Alert } from "@launchpad-ui/core";
import { Heading, Text, ProgressBar, Button, Link } from "@launchpad-ui/components";
import { Icon } from "@launchpad-ui/icons";

const DebugSessionsPage = () => {
  const navigate = useNavigate();
  const [debugSessions, setDebugSessions] = useState<DebugSession[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [totalCount, setTotalCount] = useState<number>(0);
  const [deletingSession, setDeletingSession] = useState<string | null>(null);

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

  const handleDeleteSession = async (sessionKey: string) => {
    if (!confirm(`Are you sure you want to delete debug session "${sessionKey}" and all its events? This action cannot be undone.`)) {
      return;
    }

    try {
      setDeletingSession(sessionKey);
      setError(null);

      const response = await fetch(apiRoute(`/dev/debug-sessions/${encodeURIComponent(sessionKey)}`), {
        method: 'DELETE',
      });

      if (!response.ok) {
        if (response.status === 404) {
          throw new Error('Debug session not found');
        }
        throw new Error(`Failed to delete debug session: ${response.status} ${response.statusText}`);
      }

      // Refresh the sessions list after successful deletion
      await fetchDebugSessions();
    } catch (err) {
      setError(err instanceof Error ? err.message : "An unknown error occurred while deleting the session");
    } finally {
      setDeletingSession(null);
    }
  };

  if (loading) {
    return (
      <Box padding="2rem" width="100%">
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
      <Box padding="2rem" width="100%">
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
    <Box padding="2rem" width="100%">
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
                  Debug Session Started
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
                    <Link href={`/ui/debug-sessions/${session.key}/events`}>
                      {formatDate(session.written_at)}
                    </Link>
                  </td>
                  <td style={{ padding: "0.75rem", textAlign: "right" }}>
                    <Text>
                      {session.event_count.toLocaleString()}
                    </Text>
                  </td>
                  <td style={{ padding: "0.75rem", textAlign: "center" }}>
                    <button
                      onClick={() => handleDeleteSession(session.key)}
                      disabled={deletingSession === session.key}
                      style={{
                        background: "none",
                        border: "1px solid var(--lp-color-border-destructive)",
                        borderRadius: "4px",
                        padding: "0.25rem 0.5rem",
                        cursor: deletingSession === session.key ? "not-allowed" : "pointer",
                        display: "flex",
                        alignItems: "center",
                        gap: "0.25rem",
                        color: "var(--lp-color-text-destructive)",
                        opacity: deletingSession === session.key ? 0.6 : 1
                      }}
                      title={deletingSession === session.key ? "Deleting..." : "Delete session and all events"}
                    >
                      <Icon name={"delete"} size="small" />
                    </button>
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
