import { useEffect, useState } from "react";
import { apiRoute } from "./util";
import { EventData } from "./types";

type Props = {
  limit?: number;
};

const clipboardLink = (linkText: string, value: string) => {
  return (
    <a
      href="#"
      onClick={(e) => {
        e.preventDefault();
        navigator.clipboard.writeText(value);
      }}
    >
      {linkText}
    </a>
  );
}

const summaryRows = (summaryEvent: EventData) => {
  let rows = [];
  for (const [key, value] of Object.entries((summaryEvent.data as any).features)) {
    const rowId = summaryEvent.id + key;
    const counters = (value as any).counters || [];

    for (const counter of counters) {
      rows.push(
        <tr key={rowId}>
          <td>{new Date(summaryEvent.timestamp).toLocaleTimeString()}</td>
          <td>summary</td>
          <td>{key}</td>
          <td>evaluated as {String(counter.value)}</td>
          <td>{clipboardLink('copy to clipboard', JSON.stringify(summaryEvent.data))}</td>
        </tr>
      );
    }
  }

  return rows;
}

const indexRows = (indexEvent: EventData) => {
  let eventText;
  if (indexEvent.data.context) {
    eventText = 'context kind: ' + (indexEvent.data.context?.kind || 'unknown');
  } else {
    eventText = 'unknown';
  }

  return [
    <tr key={indexEvent.id}>
      <td>{new Date(indexEvent.timestamp).toLocaleTimeString()}</td>
      <td>index</td>
      <td>n/a</td>
      <td>{eventText}</td>
      <td>{clipboardLink('copy to clipboard', JSON.stringify(indexEvent.data))}</td>
    </tr>
  ]
}

const featureRows = (featureEvent: EventData) => {
  const data = featureEvent.data as any; // Type assertion for feature event
  const eventText = `evaluated as ${String(data.value)} (variation ${data.variation})`;
  
  return [
    <tr key={featureEvent.id}>
      <td>{new Date(featureEvent.timestamp).toLocaleTimeString()}</td>
      <td>feature</td>
      <td>{data.key || 'unknown'}</td>
      <td>{eventText}</td>
      <td>{clipboardLink('copy to clipboard', JSON.stringify(featureEvent.data))}</td>
    </tr>
  ];
}

const customRows = (event: EventData) => {
  return [
    <tr key={event.id}>
      <td>{new Date(event.timestamp).toLocaleTimeString()}</td>
      <td>{event.data.kind}</td>
      <td>{event.data.key || 'unknown'}</td>
      <td>value is {(event.data as any).metricValue}</td>
      <td>{clipboardLink('copy to clipboard', JSON.stringify(event.data))}</td>
    </tr>,
  ];
}


// Return array of <tr>s:
// Time, Type, Key, Event, ViewAttributes
const renderEvent = (event: EventData) => {
  switch (event.data.kind) {
    case 'summary':
      return summaryRows(event);
    case 'index':
      return indexRows(event);
    case 'feature':
      return featureRows(event);
    case 'custom':
      return customRows(event);
    default:
      return [
        <tr key={event.id}>
          <td>{event.timestamp}</td>
          <td>{event.data.kind}</td>
          <td></td>
          <td></td>
          <td>{clipboardLink('copy to clipboard', JSON.stringify(event.data))}</td>
        </tr>,
      ];
  }
};

const EventsPage = ({ limit = 1000 }: Props) => {
  const [events, setEvents] = useState<EventData[]>([]);

  useEffect(() => {
    const eventSource = new EventSource(apiRoute('/events/tee'));

    eventSource.addEventListener('put', (event) => {
      if (!event.data || event.data.trim() === '') {
        return;
      }

      let parsed;
      try {
        parsed = JSON.parse(event.data);
      } catch (error) {
        console.error('Failed to parse event data as JSON:', error);
        return;
      }

      const newEvent: EventData = {
        id: Math.random().toString(36).slice(2, 11),
        timestamp: Date.now(),
        data: parsed
      };
      setEvents(prevEvents => [newEvent, ...prevEvents].slice(0, limit));
    });

    return () => {
      console.log('closing event source');
      eventSource.close();
    };
  }, []);

  return (
    <div>
      <h3>Events Stream (limit: {limit})</h3>
      <table className="events-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Type</th>
            <th>Key</th>
            <th>Event</th>
            <th>Link</th>
          </tr>
        </thead>
        <tbody>
          {events.map(event => renderEvent(event))}
        </tbody>
      </table>
      {events.length === 0 && <p>No events received yet...</p>}
    </div>
  );
};

export default EventsPage;
