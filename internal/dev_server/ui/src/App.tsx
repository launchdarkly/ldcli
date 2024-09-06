import './App.css';
import { useCallback, useEffect, useState } from 'react';
import Flags from './Flags.tsx';
import ProjectSelector from './ProjectSelector.tsx';
import { Box, Alert, CopyToClipboard, Inline } from '@launchpad-ui/core';
import SyncButton from './Sync.tsx';
import { LDFlagSet, LDFlagValue } from 'launchdarkly-js-client-sdk';
import {
  Heading,
  Text,
  Tooltip,
  TooltipTrigger,
  Pressable,
} from '@launchpad-ui/components';
import { Icon } from '@launchpad-ui/icons';
import { FlagVariation } from './api.ts';
import { apiRoute, sortFlags } from './util.ts';

function App() {
  const [selectedProject, setSelectedProject] = useState<string | null>(null);
  const [sourceEnvironmentKey, setSourceEnvironmentKey] = useState<
    string | null
  >(null);
  const [overrides, setOverrides] = useState<
    Record<string, { value: LDFlagValue }>
  >({});
  const [availableVariations, setAvailableVariations] = useState<
    Record<string, FlagVariation[]>
  >({});
  const [flags, setFlags] = useState<LDFlagSet | null>(null);
  const [showBanner, setShowBanner] = useState(false);

  const fetchDevFlags = useCallback(async () => {
    if (!selectedProject) {
      return
    }
    const res = await fetch(
      apiRoute(
        `/dev/projects/${selectedProject}?expand=overrides&expand=availableVariations`,
      ),
    );
    const json = await res.json();
    if (!res.ok) {
      throw new Error(`Got ${res.status}, ${res.statusText} from flag fetch`);
    }

    const {
      flagsState: flags,
      overrides,
      sourceEnvironmentKey,
      availableVariations,
    } = json;

    setFlags(sortFlags(flags));
    setOverrides(overrides);
    setSourceEnvironmentKey(sourceEnvironmentKey);
    setAvailableVariations(availableVariations);
  }, [selectedProject, setFlags, setSourceEnvironmentKey]);

  // Fetch flags / overrides on mount
  useEffect(() => {
    Promise.all([fetchDevFlags()]).catch(
      console.error.bind(console, 'error when fetching flags'),
    );
  }, [fetchDevFlags]);
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        padding: '1rem',
      }}
    >
      <Box
        style={{
          alignItems: 'center',
          width: '100%',
          maxWidth: '900px',
          minWidth: '600px',
          padding: '2rem',
          boxSizing: 'border-box',
        }}
      >
        <Box display="flex" flexDirection="column" padding="1rem" width="100%">
          {showBanner && (
            <Box marginBottom="2rem" width="100%">
              <Alert kind="error">
                <Heading>No projects.</Heading>
                <Text>Add one via</Text>
                <CopyToClipboard
                  kind="basic"
                  text="ldcli dev-server add-project --help"
                >
                  ldcli dev-server add-project --help
                </CopyToClipboard>
              </Alert>
            </Box>
          )}
          {!showBanner && (
            <Box
              display="flex"
              flexDirection="row"
              justifyContent="space-between"
              alignItems="center"
              marginBottom="2rem"
              width="100%"
            >
              <ProjectSelector
                selectedProject={selectedProject}
                setSelectedProject={setSelectedProject}
                setShowBanner={setShowBanner}
              />
              <TooltipTrigger>
                <Pressable>
                  <Inline gap="1">
                    <Icon name="bullseye-arrow" size="medium" />
                    <Text>{sourceEnvironmentKey}</Text>
                  </Inline>
                </Pressable>

                <Tooltip>Source Environment Key</Tooltip>
              </TooltipTrigger>
              <SyncButton
                selectedProject={selectedProject}
                setFlags={setFlags}
                setAvailableVariations={setAvailableVariations}
              />
            </Box>
          )}
          {selectedProject && (
            <Box width="100%">
              <Flags
                availableVariations={availableVariations}
                selectedProject={selectedProject}
                flags={flags}
                overrides={overrides}
                setOverrides={setOverrides}
              />
            </Box>
          )}
        </Box>
      </Box>
    </div>
  );
}

export default App;
