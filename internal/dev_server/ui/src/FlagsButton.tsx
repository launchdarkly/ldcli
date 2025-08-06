import {
  Button,
  Tooltip,
  TooltipTrigger,
} from '@launchpad-ui/components';
import { Icon } from '@launchpad-ui/icons';
import { Inline } from '@launchpad-ui/core';
import { useNavigate, useLocation } from 'react-router';

const FlagsButton = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const isActive = location.pathname === '/ui';

  return (
    <TooltipTrigger>
      <Button 
        onPress={() => navigate('/ui')}
        variant={isActive ? 'primary' : 'default'}
      >
        <div>
          <Inline gap="1">
            <Icon name="flag" size="small" />
            <span>Flags</span>
          </Inline>
        </div>
      </Button>
      <Tooltip>View and edit flags for the selected project</Tooltip>
    </TooltipTrigger>
  );
};

export default FlagsButton;
