import { useEffect, useState } from "react";
import { apiRoute } from "./util";
import { EventData } from "./types";
import Event from "./Event";

type Props = {
};


const renderEvent = (event: EventData) => {
  let parsed;
  try {
    parsed = JSON.parse(event.data);
  } catch (error) {
    console.error('Failed to parse event data as JSON:', error);
    return <div>Error. See console.</div>;
  }

  return <Event event={event} />;
};

const EventsPage = ({}: Props) => {
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
      setEvents(prevEvents => [...prevEvents, newEvent]);
    });

    return () => {
      console.log('closing event source');
      eventSource.close();
    };
  }, []);

  return (
    <div>
      <h3>Events Stream</h3>
      <ul>
        {events.map(event => (
          <li key={event.id}>{renderEvent(event)}</li>
        ))}
      </ul>
      {events.length === 0 && <p>No events received yet...</p>}
    </div>
  );
};

export default EventsPage;
