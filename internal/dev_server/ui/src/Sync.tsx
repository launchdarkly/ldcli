import {
  Button,
  Tooltip,
  TooltipTrigger,
  ProgressBar,
  ToastQueue,
} from '@launchpad-ui/components';
import { apiRoute, sortFlags } from './util.ts';
import { LDFlagSet } from 'launchdarkly-js-client-sdk';
import { useState } from 'react';
import { Icon } from '@launchpad-ui/icons';
import { Inline } from '@launchpad-ui/core';
import { FlagVariation } from './api.ts';

const syncProject = async (selectedProject: string) => {
  const res = await fetch(
    apiRoute(`/dev/projects/${selectedProject}?expand=availableVariations`),
    {
      method: 'PATCH',
      body: JSON.stringify({}),
    },
  );

  const json = await res.json();
  if (!res.ok) {
    throw new Error(`Got ${res.status}, ${res.statusText} from projects fetch`);
  }
  return json;
};

type Props = {
  selectedProject: string | null;
  setFlags: (flags: LDFlagSet) => void;
  setAvailableVariations: (
    availableVariations: Record<string, FlagVariation[]>,
  ) => void;
};

const SyncButton = ({
  selectedProject,
  setFlags,
  setAvailableVariations,
}: Props) => {
  const [isLoading, setIsLoading] = useState(false);

  const handleClick = async () => {
    setIsLoading(true);
    try {
      const result = await syncProject(selectedProject!);
      setAvailableVariations(result.availableVariations);
      setFlags(sortFlags(result.flagsState));
    } catch (error) {
      ToastQueue.warning('Sync failed');
      console.error('Sync failed:', error);
    } finally {
      ToastQueue.success('Sync successful');
      setIsLoading(false);
    }
  };

  if (!selectedProject) {
    return null;
  }

  return (
    <TooltipTrigger>
      <Button
        onPress={handleClick}
        isDisabled={isLoading}
        style={{ backgroundColor: isLoading ? 'lightgray' : undefined }}
      >
        {isLoading ? (
          <ProgressBar aria-label="loading" isIndeterminate />
        ) : (
          <div>
            <Inline gap="1">
              <Icon name="sync" size="small" />
              <span>Sync</span>
            </Inline>
          </div>
        )}
      </Button>
      <Tooltip>Sync the selected project from the source environment</Tooltip>
    </TooltipTrigger>
  );
};

export default SyncButton;
