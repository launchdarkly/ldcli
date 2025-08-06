import {
  Button,
  Tooltip,
  TooltipTrigger,
} from '@launchpad-ui/components';
import { Icon } from '@launchpad-ui/icons';
import { Inline } from '@launchpad-ui/core';
import { useNavigate, useLocation } from 'react-router';

const EventsButton = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const isActive = location.pathname === '/events';

  return (
    <TooltipTrigger>
      <Button 
        onPress={() => navigate('/events')}
        variant={isActive ? 'primary' : 'default'}
      >
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
