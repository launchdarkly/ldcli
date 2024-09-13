import './App.css';
import { useCallback, useEffect, useState } from 'react';
import Flags from './Flags.tsx';
import ProjectSelector from './ProjectSelector.tsx';
import { Box, Alert, CopyToClipboard } from '@launchpad-ui/core';
import SyncButton from './Sync.tsx';
import { LDFlagSet, LDFlagValue } from 'launchdarkly-js-client-sdk';
import { Heading, Text } from '@launchpad-ui/components';
import { FlagVariation } from './api.ts';
import { apiRoute, sortFlags } from './util.ts';
import { ProjectEditor } from './ProjectEditor';

interface Environment {
  key: string;
  name: string;
}

function App() {
  const [selectedProject, setSelectedProject] = useState<string | null>(null);
  const [selectedEnvironment, setSelectedEnvironment] =
    useState<Environment | null>(null);
  const [sourceEnvironmentKey, setSourceEnvironmentKey] = useState<
    string | null
  >(null);
  const [overrides, setOverrides] = useState<
    Record<string, { value: LDFlagValue; version: number }>
  >({});
  const [availableVariations, setAvailableVariations] = useState<
    Record<string, FlagVariation[]>
  >({});
  const [flags, setFlags] = useState<LDFlagSet | null>(null);
  const [showBanner, setShowBanner] = useState(false);
  const [context, setContext] = useState<string>('{}');

  const fetchDevFlags = useCallback(async () => {
    if (!selectedProject) {
      return;
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
      context: fetchedContext,
    } = json;

    setFlags(sortFlags(flags));
    setOverrides(overrides);
    setSourceEnvironmentKey(sourceEnvironmentKey);
    setAvailableVariations(availableVariations);
    setContext(JSON.stringify(fetchedContext || `{}`, null, 2));

    // Fetch the environment details and set the selectedEnvironment
    const environments = await fetchEnvironments(selectedProject);
    const currentEnvironment = environments.find(
      (env: Environment) => env.key === sourceEnvironmentKey,
    );
    if (currentEnvironment) {
      setSelectedEnvironment(currentEnvironment);
    }
  }, [selectedProject]);

  useEffect(() => {
    if (selectedProject) {
      fetchDevFlags().catch(
        console.error.bind(console, 'error when fetching flags'),
      );
    }
  }, [fetchDevFlags, selectedProject]);

  // Fetch flags / overrides on mount
  useEffect(() => {
    Promise.all([fetchDevFlags()]).catch(
      console.error.bind(console, 'error when fetching flags'),
    );
  }, [fetchDevFlags]);

  const updateProjectSettings = useCallback(
    async (newEnvironment: Environment | null, newContext: string) => {
      if (!selectedProject) {
        return;
      }
      try {
        const res = await fetch(apiRoute(`/dev/projects/${selectedProject}`), {
          method: 'PATCH',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            sourceEnvironmentKey: newEnvironment?.key,
            context: JSON.parse(newContext),
          }),
        });

        if (!res.ok) {
          throw new Error(
            `Got ${res.status}, ${res.statusText} from project settings update`,
          );
        }

        const json = await res.json();
        const {
          flagsState: flags,
          sourceEnvironmentKey,
          context: fetchedContext,
        } = json;

        setFlags(sortFlags(flags));
        setSourceEnvironmentKey(sourceEnvironmentKey);
        setContext(JSON.stringify(fetchedContext || {}, null, 2));
        setSelectedEnvironment(newEnvironment);

        // Fetch updated flags and variations
        await fetchDevFlags();
      } catch (error) {
        console.error('Error updating project settings:', error);
        // You might want to show an error message to the user here
      }
    },
    [selectedProject, fetchDevFlags],
  );

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
              {selectedProject && (
                <ProjectEditor
                  projectKey={selectedProject}
                  selectedEnvironment={selectedEnvironment}
                  setSelectedEnvironment={setSelectedEnvironment}
                  sourceEnvironmentKey={sourceEnvironmentKey}
                  context={context}
                  updateProjectSettings={updateProjectSettings}
                />
              )}
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
                setOverrides={(
                  newOverrides: Record<
                    string,
                    { value: LDFlagValue; version: number }
                  >,
                ) => {
                  setOverrides(newOverrides);
                }}
              />
            </Box>
          )}
        </Box>
      </Box>
    </div>
  );
}

async function fetchEnvironments(projectKey: string) {
  const res = await fetch(apiRoute(`/dev/projects/${projectKey}/environments`));
  if (!res.ok) {
    throw new Error(
      `Got ${res.status}, ${res.statusText} from environments fetch`,
    );
  }
  return res.json();
}

export default App;
