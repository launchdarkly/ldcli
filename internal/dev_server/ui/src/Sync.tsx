import {
  Button,
  Tooltip,
  TooltipTrigger,
  ProgressBar,
} from '@launchpad-ui/components';
import { apiRoute, sortFlags } from './util.ts';
import { LDFlagSet } from 'launchdarkly-js-client-sdk';
import { useState } from 'react';

const syncProject = async (selectedProject: string) => {
  const res = await fetch(apiRoute(`/dev/projects/${selectedProject}/sync`), {
    method: 'PATCH',
  });

  const json = await res.json();
  if (!res.ok) {
    throw new Error(`Got ${res.status}, ${res.statusText} from projects fetch`);
  }
  return json;
};

const SyncButton = ({
  selectedProject,
  setFlags,
}: {
  selectedProject: string | null;
  setFlags: (flags: LDFlagSet) => void;
}) => {
  const [isLoading, setIsLoading] = useState(false);

  const handleClick = async () => {
    setIsLoading(true);
    try {
      const result = await syncProject(selectedProject!);
      setFlags(sortFlags(result.flagsState));
    } catch (error) {
      console.error('Sync failed:', error);
    } finally {
      setIsLoading(false);
    }
  };

  if (!selectedProject) {
    return null;
  }

  return (
    <TooltipTrigger>
      <Button onClick={handleClick} disabled={isLoading}>
        {isLoading ? (
          <ProgressBar aria-label="loading" isIndeterminate />
        ) : (
          'Sync'
        )}
      </Button>
      <Tooltip>Sync the selected project from the source environment</Tooltip>
    </TooltipTrigger>
  );
};

export default SyncButton;
