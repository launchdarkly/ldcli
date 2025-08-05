import {
  Button,
  Tooltip,
  TooltipTrigger,
} from '@launchpad-ui/components';
import { Icon } from '@launchpad-ui/icons';
import { Inline } from '@launchpad-ui/core';

type Props = {
  onPress: () => void;
};

const EventsButton = ({ onPress }: Props) => {
  return (
    <TooltipTrigger>
      <Button onPress={onPress}>
        <div>
          <Inline gap="1">
            <Icon name="play" size="small" />
            <span>Events</span>
          </Inline>
        </div>
      </Button>
      <Tooltip>View events</Tooltip>
    </TooltipTrigger>
  );
};

export default EventsButton;
