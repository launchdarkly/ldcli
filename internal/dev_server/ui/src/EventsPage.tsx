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

const summaryRows = (summaryEvent: any) => {
  let parsed;
  try {
    parsed = JSON.parse(summaryEvent.data);
  } catch (error) {
    console.error('Failed to parse event data as JSON:', error);
    return <div>Error. See console.</div>;
  }
  console.log('parsed', parsed);

  let rows = [];
  for (const [key, value] of Object.entries(parsed.features)) {
    const rowId = summaryEvent.id + key;
    const counters = (value as any).counters || [];

    for (const counter of counters) {
      rows.push(
        <tr key={rowId}>
          <td>{new Date(summaryEvent.timestamp).toLocaleTimeString()}</td>
          <td>summary</td>
          <td>{key}</td>
          <td>evaluated as {String(counter.value)}</td>
          <td>{clipboardLink('copy to clipboard', JSON.stringify(parsed))}</td>
        </tr>
      );
    }
  }

  return rows;
}

// Return array of <tr>s:
// Time, Type, Key, Event, ViewAttributes
const renderEvent = (event: EventData) => {
  let parsed;
  try {
    parsed = JSON.parse(event.data);
  } catch (error) {
    console.error('Failed to parse event data as JSON:', error);
    return <div>Error. See console.</div>;
  }

  if (parsed.kind === 'summary') {
    return summaryRows(event);
  }

  return [
    <tr key={event.id}>
      <td>{event.timestamp}</td>
      <td>{parsed.kind}</td>
      <td></td>
      <td>{parsed.kind}</td>
      <td></td>
    </tr>,
  ];
};

const EventsPage = ({ limit = 1000 }: Props) => {
  const [events, setEvents] = useState<EventData[]>([]);

  useEffect(() => {
    const eventSource = new EventSource(apiRoute('/events/tee'));

    eventSource.addEventListener('put', (event) => {
      if (!event.data || event.data.trim() === '') {
        return;
      }
      const newEvent: EventData = {
        id: Math.random().toString(36).slice(2, 11),
        timestamp: Date.now(),
        data: event.data
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
