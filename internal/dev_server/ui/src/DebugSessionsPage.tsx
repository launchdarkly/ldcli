import { useEffect, useState } from 'react';
import { apiRoute } from './util';
import {
  DebugSession,
  DebugSessionsPage as DebugSessionsPageType,
} from './types';
import { Box, Alert } from '@launchpad-ui/core';
import {
  Button,
  Cell,
  Column,
  Heading,
  Link,
  ProgressBar,
  Row,
  Table,
  TableBody,
  TableHeader,
  Text,
} from '@launchpad-ui/components';
import { Icon } from '@launchpad-ui/icons';

const DebugSessionsPage = () => {
  const [debugSessions, setDebugSessions] = useState<DebugSession[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [totalCount, setTotalCount] = useState<number>(0);
  const [deletingSession, setDeletingSession] = useState<string | null>(null);

  const fetchDebugSessions = async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await fetch(apiRoute('/dev/debug-sessions?limit=100'));

      if (!response.ok) {
        throw new Error(
          `Failed to fetch debug sessions: ${response.status} ${response.statusText}`,
        );
      }

      const data: DebugSessionsPageType = await response.json();
      setDebugSessions(data.sessions || []);
      setTotalCount(data.total_count || 0);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'An unknown error occurred',
      );
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
    if (
      !confirm(
        `Are you sure you want to delete debug session "${sessionKey}" and all its events? This action cannot be undone.`,
      )
    ) {
      return;
    }

    try {
      setDeletingSession(sessionKey);
      setError(null);

      const response = await fetch(
        apiRoute(`/dev/debug-sessions/${encodeURIComponent(sessionKey)}`),
        {
          method: 'DELETE',
        },
      );

      if (!response.ok) {
        if (response.status === 404) {
          throw new Error('Debug session not found');
        }
        throw new Error(
          `Failed to delete debug session: ${response.status} ${response.statusText}`,
        );
      }

      // Refresh the sessions list after successful deletion
      await fetchDebugSessions();
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : 'An unknown error occurred while deleting the session',
      );
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
      <Box
        display="flex"
        justifyContent="space-between"
        alignItems="center"
        marginBottom="1rem"
      >
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
        <Box
          display="flex"
          flexDirection="column"
          width="100%"
          minWidth="600px"
          borderRadius="4px"
          borderWidth="1px"
          borderColor="var(--lp-color-border-ui-primary)"
        >
          <Table>
            <TableHeader>
              <Column isRowHeader>Debug Session Started</Column>
              <Column>Event Count</Column>
              <Column>Actions</Column>
            </TableHeader>
            <TableBody>
              {debugSessions.map((session) => (
                <Row key={session.key}>
                  <Cell>
                    <Link href={`/ui/debug-sessions/${session.key}/events`}>
                      {formatDate(session.written_at)}
                    </Link>
                  </Cell>
                  <Cell>
                    <Text>{session.event_count.toLocaleString()}</Text>
                  </Cell>
                  <Cell>
                    <Button
                      isDisabled={deletingSession === session.key}
                      variant="destructive"
                      onPress={() => handleDeleteSession(session.key)}
                    >
                      <Icon name={'delete'} size="small" />
                    </Button>
                  </Cell>
                </Row>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
    </Box>
  );
};

export default DebugSessionsPage;
