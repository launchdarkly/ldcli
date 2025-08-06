import { EventData } from "./types";
import { Icon } from "@launchpad-ui/icons";
import { useState } from "react";

type Props = {
  events: EventData[];
  limit: number;
  onToggleStreaming?: (newStreamingState: boolean) => void;
};

const clipboardLink = (linkText: string, value: string, showNotification: (message: string) => void) => {
  return (
    <a
      href="#"
      onClick={(e) => {
        e.preventDefault();
        navigator.clipboard.writeText(value).then(() => {
          showNotification("Copied to clipboard!");
        }).catch(() => {
          showNotification("Failed to copy to clipboard");
        });
      }}
    >
      {linkText}
    </a>
  );
}

const summaryRows = (summaryEvent: EventData, showNotification: (message: string) => void) => {
  let rows = [];
  for (const [key, value] of Object.entries((summaryEvent.data as any).features)) {
    const rowId = summaryEvent.id + key;
    const counters = (value as any).counters || [];

    for (const counter of counters) {
      rows.push(
        <tr key={rowId}>
          <td>{new Date(summaryEvent.timestamp).toLocaleTimeString()}</td>
          <td>summary</td>
          <td><Icon name="flag" size="small" /> {key}</td>
          <td>evaluated as {String(counter.value)}</td>
          <td>{clipboardLink('Copy to clipboard', JSON.stringify(summaryEvent.data), showNotification)}</td>
        </tr>
      );
    }
  }

  return rows;
}

const indexRows = (indexEvent: EventData, showNotification: (message: string) => void) => {
  let eventText;
  if (indexEvent.data.context) {
    eventText = (indexEvent.data.context?.kind || 'unknown') + ' context';
  } else if (indexEvent.data.user) {
    eventText = (indexEvent.data.user.key || 'unknown') + ' user';
  }
  else {
    eventText = 'unknown context';
  }

  return [
    <tr key={indexEvent.id}>
      <td>{new Date(indexEvent.timestamp).toLocaleTimeString()}</td>
      <td>index</td>
      <td><Icon name="metric-funnel" size="small" /> {JSON.stringify(indexEvent.data).length} bytes</td>
      <td>{eventText}</td>
      <td>{clipboardLink('Copy to clipboard', JSON.stringify(indexEvent.data), showNotification)}</td>
    </tr>
  ]
}

const featureRows = (featureEvent: EventData, showNotification: (message: string) => void) => {
  const data = featureEvent.data as any; // Type assertion for feature event
  const eventText = `evaluated as ${String(data.value)}`;

  return [
    <tr key={featureEvent.id} className="feature-row">
      <td>{new Date(featureEvent.timestamp).toLocaleTimeString()}</td>
      <td>feature</td>
      <td>{data.key || 'unknown'}</td>
      <td>{eventText}</td>
      <td>{clipboardLink('Copy to clipboard', JSON.stringify(featureEvent.data), showNotification)}</td>
    </tr>
  ];
}

const customRows = (event: EventData, showNotification: (message: string) => void) => {
  return [
    <tr key={event.id}>
      <td>{new Date(event.timestamp).toLocaleTimeString()}</td>
      <td>{event.data.kind}</td>
      <td><Icon name="chart-histogram" size="small" /> {event.data.key || 'unknown'}</td>
      <td>value is {(event.data as any).metricValue}</td>
      <td>{clipboardLink('Copy to clipboard', JSON.stringify(event.data), showNotification)}</td>
    </tr>,
  ];
}


// Return array of <tr>s:
// Time, Type, Key, Event, ViewAttributes
const renderEvent = (event: EventData, showNotification: (message: string) => void) => {
  switch (event.data.kind) {
    case 'summary':
      return summaryRows(event, showNotification);
    case 'index':
      return indexRows(event, showNotification);
    case 'feature':
      return featureRows(event, showNotification);
    case 'custom':
      return customRows(event, showNotification);
    default:
      return [
        <tr key={event.id}>
          <td>{(() => {
            try {
              const date = new Date(event.timestamp);
              return isNaN(date.getTime()) ? event.timestamp : date.toLocaleTimeString();
            } catch {
              return event.timestamp;
            }
          })()}</td>
          <td>{event.data.kind}</td>
          <td></td>
          <td></td>
          <td>{clipboardLink('Copy to clipboard', JSON.stringify(event.data), showNotification)}</td>
        </tr>,
      ];
  }
};

const EventsTable = ({
  events,
  limit,
  onToggleStreaming
}: Props) => {
  const [notification, setNotification] = useState<string | null>(null);
  const [isStreaming, setIsStreaming] = useState<boolean>(true);

  const handleToggleStreaming = (newStreamingState: boolean) => {
    setIsStreaming(newStreamingState);
    onToggleStreaming?.(newStreamingState);
  };

  const showNotification = (message: string) => {
    setNotification(message);
    setTimeout(() => {
      setNotification(null);
    }, 1500);
  };

  return (
    <div>
      <h3>Events Stream (limit: {limit})</h3>
      {onToggleStreaming && (
        <button
          className={`streaming-toggle-button ${isStreaming ? 'streaming' : 'not-streaming'}`}
          onClick={() => handleToggleStreaming(!isStreaming)}
        >
          {isStreaming ? 'Streaming ON' : 'Streaming OFF'}
        </button>
      )}
      <table className="events-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Type</th>
            <th>Target</th>
            <th>Event</th>
            <th>Link</th>
          </tr>
        </thead>
        <tbody>
          {events.map(event => renderEvent(event, showNotification))}
        </tbody>
      </table>
      {events.length === 0 && <p>No events received yet...</p>}
      {notification && (
        <div className={`copy-notification ${notification ? 'show' : 'hide'}`}>
          {notification}
        </div>
      )}
    </div>
  );
};

export default EventsTable;