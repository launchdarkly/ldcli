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
        <Inline gap="2">
          <Box display="flex" alignItems="center">
            <ProgressBar aria-label="loading" isIndeterminate />
            <span style={{ marginLeft: '0.5rem' }}>Updating...</span>
          </Box>
        </Inline>
      ) : selectedEnvironment ? (
        <Inline gap="2">
          <Icon
            name="bullseye-arrow"
            size="medium"
            data-testid="icon-bullseye-arrow"
          />
          <span>{selectedEnvironment}</span>
        </Inline>
      ) : (
        'Environment'
      )}
    </Button>
  );
}
