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

const FlagsButton = ({ onPress }: Props) => {
  return (
    <TooltipTrigger>
      <Button onPress={onPress}>
        <div>
          <Inline gap="1">
            <Icon name="flag" size="small" />
            <span>Flags</span>
          </Inline>
        </div>
      </Button>
      <Tooltip>View flags</Tooltip>
    </TooltipTrigger>
  );
};

export default FlagsButton;
