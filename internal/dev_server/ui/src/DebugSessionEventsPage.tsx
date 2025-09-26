import { useEffect, useState } from 'react';
import { useParams } from 'react-router';
import { apiRoute } from './util';
import { ApiEventsPage, EventData, convertApiEventToEventData } from './types';
import { Box, Alert } from '@launchpad-ui/core';
import { Heading, Text, ProgressBar, Button } from '@launchpad-ui/components';
import { Icon } from '@launchpad-ui/icons';
import EventsTable from './EventsTable';
import { TextField, Label, Input } from '@launchpad-ui/components';
import { Fragment } from 'react';

const DebugSessionEventsPage = () => {
  const { debugSessionKey } = useParams<{ debugSessionKey: string }>();
  const [events, setEvents] = useState<EventData[]>([]);
  const [displayedEvents, setDisplayedEvents] = useState<EventData[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  const fetchEvents = async () => {
    if (!debugSessionKey) {
      setError('Debug session key is required');
      setLoading(false);
      return;
    }

    try {
      setLoading(true);
      setError(null);

      const response = await fetch(
        apiRoute(
          `/dev/debug-sessions/${encodeURIComponent(debugSessionKey)}/events?limit=1000`,
        ),
      );

      if (!response.ok) {
        throw new Error(
          `Failed to fetch events: ${response.status} ${response.statusText}`,
        );
      }

      const data: ApiEventsPage = await response.json();
      const convertedEvents =
        data.events?.map(convertApiEventToEventData) || [];
      setEvents(convertedEvents);
      setDisplayedEvents(convertedEvents);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'An unknown error occurred',
      );
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchEvents();
  }, [debugSessionKey]);

  const handleSearchChange = (value: string) => {
    setDisplayedEvents(
      events.filter((event) => {
        let search = '';

        const extractValues = (obj: unknown): string[] => {
          if (obj === null || obj === undefined) return [];
          if (
            typeof obj === 'string' ||
            typeof obj === 'number' ||
            typeof obj === 'boolean'
          ) {
            return [String(obj)];
          }
          if (Array.isArray(obj)) {
            return obj.flatMap((item) => extractValues(item));
          }
          if (typeof obj === 'object') {
            return Object.values(obj).flatMap((value) => extractValues(value));
          }
          return [];
        };
        search = extractValues(event).join(' ');

        return search.toLowerCase().includes(value.toLowerCase());
      }),
    );
  };

  if (loading) {
    return (
      <Box padding="2rem" width="100%">
        <Heading>Debug Session Events</Heading>
        <Box marginTop="1rem">
          <Text color="var(--lp-color-text-ui-secondary)">
            Session: {debugSessionKey}
          </Text>
        </Box>
        <Box marginTop="1rem">
          <ProgressBar isIndeterminate />
        </Box>
        <Box marginTop="1rem">
          <Text>Loading events...</Text>
        </Box>
      </Box>
    );
  }

  if (error) {
    return (
      <Box padding="2rem" width="100%">
        <Heading>Debug Session Events</Heading>
        <Box marginTop="1rem">
          <Text color="var(--lp-color-text-ui-secondary)">
            Session: {debugSessionKey}
          </Text>
        </Box>
        <Box marginTop="1rem">
          <Alert kind="error">
            <Text>Error: {error}</Text>
          </Alert>
        </Box>
        <Box marginTop="1rem">
          <Button onPress={fetchEvents}>Retry</Button>
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
        <Box>
          <Heading>Debug Session Events</Heading>
          <Box marginTop="0.5rem">
            <Text
              color="var(--lp-color-text-ui-secondary)"
              style={{ fontFamily: 'monospace' }}
            >
              Session: {debugSessionKey}
            </Text>
          </Box>
        </Box>
      </Box>

      <TextField onChange={handleSearchChange} name="debug-session-search">
        <Fragment key=".0">
          <Label>Search</Label>
          <Input placeholder="Try a type like 'summary', or an email address, or similar" />
        </Fragment>
      </TextField>

      {events.length === 0 ? (
        <Box
          padding="2rem"
          textAlign="center"
          backgroundColor="var(--lp-color-bg-ui-secondary)"
          borderRadius="4px"
        >
          <Icon name="data" size="large" />
          <Box marginTop="1rem">
            <Text>No events found for this debug session</Text>
          </Box>
          <Box marginTop="0.5rem">
            <Text color="var(--lp-color-text-ui-secondary)">
              Events will appear here when they are captured for this session
            </Text>
          </Box>
        </Box>
      ) : (
        <EventsTable events={displayedEvents} />
      )}
    </Box>
  );
};

export default DebugSessionEventsPage;
