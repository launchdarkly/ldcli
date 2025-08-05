import { useEffect, useState } from "react";
import { apiRoute } from "./util";

type Props = {
};

interface EventData {
  id: string;
  timestamp: number;
  data: string;
}

const EventsPage = ({}: Props) => {
  const [events, setEvents] = useState<EventData[]>([]);

  useEffect(() => {
    console.log(apiRoute('/events/tee'));
    const eventSource = new EventSource(apiRoute('/events/tee'));

    eventSource.addEventListener('put', (event) => {
      if (!event.data || event.data.trim() === '') {
        return;
      }
      const newEvent: EventData = {
        id: Math.random().toString(36).substr(2, 9),
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
        {events.map((event) => {
          let displayData = event.data;
          try {
            // Try to parse and prettify JSON
            const parsed = JSON.parse(event.data);
            displayData = JSON.stringify(parsed, null, 2);
          } catch {
            // If not JSON, keep original data
            displayData = event.data;
          }
          
          return (
            <li key={event.id} style={{ marginBottom: '10px' }}>
              <div>
                <strong>{new Date(event.timestamp).toLocaleTimeString()}</strong>
              </div>
              <div style={{ marginLeft: '10px', marginTop: '5px' }}>
                <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                  {displayData}
                </pre>
              </div>
            </li>
          );
        })}
      </ul>
      {events.length === 0 && <p>No events received yet...</p>}
    </div>
  );
};

export default EventsPage;
