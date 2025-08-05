import { EventData } from "./types";

type Props = {
    event: EventData;
}

const Event = ({ event }: Props) => {
    const parsed = JSON.parse(event.data);
    console.log('parsed', parsed);
    return <span>{event.data}</span>
}

export default Event;