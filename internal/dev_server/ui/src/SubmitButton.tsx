import { Button, ProgressBar } from '@launchpad-ui/components';
import { Box, Inline } from '@launchpad-ui/core';
import { Icon } from '@launchpad-ui/icons';

type Props = {
  isSubmitting: boolean;
  selectedEnvironment: string | null;
};

export function ProjectEditButton({
  isSubmitting,
  selectedEnvironment,
}: Props) {
  return (
    <Button variant="primary" type="submit" isDisabled={isSubmitting}>
      {isSubmitting ? (
        <Inline gap="2" style={{ alignItems: 'center' }}>
          <Box display="flex" alignItems="center">
            <ProgressBar
              aria-label="loading"
              isIndeterminate
              style={{ width: '16px', height: '16px' }}
            />
            <span>Updating...</span>
          </Box>
        </Inline>
      ) : selectedEnvironment ? (
        <Inline gap="2">
          <Icon name="bullseye-arrow" size="medium" />
          <span>{selectedEnvironment}</span>
        </Inline>
      ) : (
        'Environment'
      )}
    </Button>
  );
}
