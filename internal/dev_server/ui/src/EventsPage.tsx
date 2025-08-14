import { useEffect, useState } from 'react';
import { apiRoute } from './util';
import { EventData } from './types';
import EventsTable from './EventsTable';

type Props = {
  limit?: number;
};

const EventsPage = ({ limit = 1000 }: Props) => {
  const [events, setEvents] = useState<EventData[]>([]);
  const [backlog, setBacklog] = useState<EventData[]>([]);
  const [isStreaming, setIsStreaming] = useState<boolean>(true);

  useEffect(() => {
    const eventSource = new EventSource(apiRoute('/events/tee'));

    eventSource.addEventListener('put', (event: MessageEvent) => {
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
        data: parsed,
      };

      if (isStreaming) {
        setEvents((prevEvents) => [newEvent, ...prevEvents].slice(0, limit));
      } else {
        setBacklog((prevBacklog) => [newEvent, ...prevBacklog].slice(0, limit));
      }
    });

    return () => {
      console.log('closing event source');
      eventSource.close();
    };
  }, [isStreaming, limit]);

  const toggleStreaming = (newStreamingState: boolean) => {
    setIsStreaming(newStreamingState);

    if (newStreamingState && backlog.length > 0) {
      // Flush backlog into events when turning streaming back on
      setEvents((prevEvents) => [...backlog, ...prevEvents].slice(0, limit));
      setBacklog([]);
    }
  };

  return <EventsTable events={events} onToggleStreaming={toggleStreaming} />;
};

export default EventsPage;
