import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router";
import { apiRoute } from "./util";
import { ApiEventsPage, EventData, convertApiEventToEventData } from "./types";
import { Box, Alert } from "@launchpad-ui/core";
import { Heading, Text, ProgressBar, Button } from "@launchpad-ui/components";
import { Icon } from "@launchpad-ui/icons";
import EventsTable from "./EventsTable";

const DebugSessionEventsPage = () => {
  const { debugSessionKey } = useParams<{ debugSessionKey: string }>();
  const navigate = useNavigate();
  const [events, setEvents] = useState<EventData[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [totalCount, setTotalCount] = useState<number>(0);

  const fetchEvents = async () => {
    if (!debugSessionKey) {
      setError("Debug session key is required");
      setLoading(false);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      
      const response = await fetch(apiRoute(`/dev/debug-sessions/${encodeURIComponent(debugSessionKey)}/events?limit=1000`));
      
      if (!response.ok) {
        throw new Error(`Failed to fetch events: ${response.status} ${response.statusText}`);
      }
      
      const data: ApiEventsPage = await response.json();
      const convertedEvents = data.events.map(convertApiEventToEventData);
      setEvents(convertedEvents);
      setTotalCount(data.total_count);
    } catch (err) {
      setError(err instanceof Error ? err.message : "An unknown error occurred");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchEvents();
  }, [debugSessionKey]);

  const handleBackToSessions = () => {
    navigate("/ui/debug-sessions");
  };

  if (loading) {
    return (
      <Box padding="2rem">
        <Box display="flex" alignItems="center" marginBottom="1rem">
          <Button onPress={handleBackToSessions} variant="minimal">
            <Icon name="arrow-left" size="small" />
            <Text marginLeft="0.5rem">Back to Debug Sessions</Text>
          </Button>
        </Box>
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
      <Box padding="2rem">
        <Box display="flex" alignItems="center" marginBottom="1rem">
          <Button onPress={handleBackToSessions} variant="minimal">
            <Icon name="arrow-left" size="small" />
            <Text marginLeft="0.5rem">Back to Debug Sessions</Text>
          </Button>
        </Box>
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
    <Box padding="2rem">
      <Box display="flex" alignItems="center" marginBottom="1rem">
        <Button onPress={handleBackToSessions} variant="minimal">
          <Icon name="arrow-left" size="small" />
          <Text marginLeft="0.5rem">Back to Debug Sessions</Text>
        </Button>
      </Box>
      
      <Box display="flex" justifyContent="space-between" alignItems="center" marginBottom="1rem">
        <Box>
          <Heading>Debug Session Events</Heading>
          <Box marginTop="0.5rem">
            <Text color="var(--lp-color-text-ui-secondary)" style={{ fontFamily: "monospace" }}>
              Session: {debugSessionKey}
            </Text>
          </Box>
        </Box>
        <Text color="var(--lp-color-text-ui-secondary)">
          {totalCount} total event{totalCount !== 1 ? 's' : ''}
        </Text>
      </Box>

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
        <EventsTable events={events} />
      )}
    </Box>
  );
};

export default DebugSessionEventsPage;
