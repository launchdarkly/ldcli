import {
  EventData,
  FeatureEventPayload,
  GenericEventPayload,
  IndexEventPayload,
  SummaryEventPayload,
} from './types';

import {
  Button,
  Cell,
  Column,
  Row,
  Table,
  TableBody,
  TableHeader,
} from '@launchpad-ui/components';
import { Box, CopyToClipboard } from '@launchpad-ui/core';
import { Icon } from '@launchpad-ui/icons';
import { useState } from 'react';

type Props = {
  events: EventData[];
  onToggleStreaming?: (newStreamingState: boolean) => void;
};

const clipboardLink = (linkText: string, value: string) => {
  return (
    <CopyToClipboard kind="basic" text={value}>
      {linkText}
    </CopyToClipboard>
  );
};

const summaryRows = (event: EventData, summaryEvent: SummaryEventPayload) => {
  const rows = [];
  for (const [key, value] of Object.entries(summaryEvent.features || {})) {
    const rowId = event.id + key;
    const counters = value.counters || [];

    for (const counter of counters) {
      rows.push(
        <Row key={rowId}>
          <Cell>{new Date(event.timestamp).toLocaleTimeString()}</Cell>
          <Cell>summary</Cell>
          <Cell>
            <Icon name="flag" size="small" /> {key}
          </Cell>
          <Cell>evaluated as {String(counter.value)}</Cell>
          <Cell>
            {clipboardLink('Copy to clipboard', JSON.stringify(summaryEvent))}
          </Cell>
        </Row>,
      );
    }
  }

  return rows;
};

const indexRows = (event: EventData, indexEvent: IndexEventPayload) => {
  let targetText = 'unknown';
  let iconName:
    | 'person'
    | 'chart-dashboard'
    | 'person-outline'
    | 'group'
    | 'cloud'
    | 'help' = 'help';
  if (event.data.context) {
    const context = indexEvent.context;
    if (!context) {
      console.error('Index event context is undefined');
      return [];
    }
    switch (context.kind) {
      case 'user':
        targetText = 'user context';
        iconName = 'person';
        break;
      case 'application':
        targetText = context.key || 'unknown application';
        iconName = 'cloud';
        break;
      case 'multi':
        if (context.user) {
          targetText = context.user.email || context.user.key || 'unknown user';
          iconName = 'person';
        } else if (context.account) {
          targetText =
            context.account.name || context.account.key || 'unknown account';
          iconName = 'group';
        } else if (context.application) {
          targetText = context.application.key || 'unknown application';
          iconName = 'cloud';
        } else {
          targetText = 'multi context';
          iconName = 'chart-dashboard';
        }
        break;
    }
  } else if (indexEvent.user) {
    targetText = (indexEvent.user.key || 'unknown') + ' user';
    iconName = 'person-outline';
  } else {
    targetText = 'unknown';
  }

  return [
    <Row key={event.id}>
      <Cell>{new Date(event.timestamp).toLocaleTimeString()}</Cell>
      <Cell>index</Cell>
      <Cell>
        <Icon name={iconName} size="small" /> {targetText}
      </Cell>
      <Cell>indexed {JSON.stringify(indexEvent).length} bytes</Cell>
      <Cell>
        {clipboardLink('Copy to clipboard', JSON.stringify(indexEvent.data))}
      </Cell>
    </Row>,
  ];
};

const featureRows = (event: EventData, featureEvent: FeatureEventPayload) => {
  const eventText = `evaluated as ${String(featureEvent.value)}`;

  return [
    <Row key={event.id} className="feature-row">
      <Cell>{new Date(event.timestamp).toLocaleTimeString()}</Cell>
      <Cell>feature</Cell>
      <Cell>{featureEvent.key || 'unknown'}</Cell>
      <Cell>{eventText}</Cell>
      <Cell>
        {clipboardLink('Copy to clipboard', JSON.stringify(featureEvent))}
      </Cell>
    </Row>,
  ];
};

const customRows = (event: EventData, customEvent: GenericEventPayload) => {
  return [
    <Row key={event.id}>
      <Cell>{new Date(event.timestamp).toLocaleTimeString()}</Cell>
      <Cell>{event.data.kind}</Cell>
      <Cell>
        <Icon name="chart-histogram" size="small" /> {customEvent.key}
      </Cell>
      <Cell>value is {customEvent.metricValue}</Cell>
      <Cell>
        {clipboardLink('Copy to clipboard', JSON.stringify(customEvent))}
      </Cell>
    </Row>,
  ];
};

// Return array of <tr>s:
// Time, Type, Key, Event, ViewAttributes
const renderEvent = (event: EventData) => {
  switch (event.data.kind) {
    case 'summary':
      return summaryRows(event, event.data as SummaryEventPayload);
    case 'index':
      return indexRows(event, event.data as IndexEventPayload);
    case 'feature':
      return featureRows(event, event.data as FeatureEventPayload);
    case 'custom':
      return customRows(event, event.data as GenericEventPayload);
    default:
      return [
        <Row key={event.id}>
          <Cell>
            {(() => {
              try {
                const date = new Date(event.timestamp);
                return isNaN(date.getTime())
                  ? event.timestamp
                  : date.toLocaleTimeString();
              } catch {
                return event.timestamp;
              }
            })()}
          </Cell>
          <Cell>{event.data.kind}</Cell>
          <Cell></Cell>
          <Cell></Cell>
          <Cell>
            {clipboardLink('Copy to clipboard', JSON.stringify(event.data))}
          </Cell>
        </Row>,
      ];
  }
};

const EventsTable = ({ events, onToggleStreaming }: Props) => {
  const [isStreaming, setIsStreaming] = useState<boolean>(true);

  const handleToggleStreaming = (newStreamingState: boolean) => {
    setIsStreaming(newStreamingState);
    onToggleStreaming?.(newStreamingState);
  };

  return (
    <Box display="flex" flexDirection="column" width="100%" minWidth="600px">
      <h3>Events Stream</h3>
      <Box paddingBottom="1rem">
        {onToggleStreaming && (
          <Button
            variant="primary"
            onPress={async () => handleToggleStreaming(!isStreaming)}
          >
            {isStreaming ? 'Streaming ON' : 'Streaming OFF'}
          </Button>
        )}
      </Box>
      <Table>
        <TableHeader>
          <Column isRowHeader>Time</Column>
          <Column>Type</Column>
          <Column>Target</Column>
          <Column>Event</Column>
          <Column>Link</Column>
        </TableHeader>
        <TableBody>{events.map((event) => renderEvent(event))}</TableBody>
      </Table>
      {events.length === 0 && <p>No events received yet...</p>}
    </Box>
  );
};

export default EventsTable;
