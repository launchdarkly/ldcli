import './App.css';
import { useState } from 'react';
import Flags from './Flags.tsx';
import ProjectSelector from './ProjectSelector.tsx';
import { Box, Alert, CopyToClipboard, Inline } from '@launchpad-ui/core';
import SyncButton from './Sync.tsx';
import { LDFlagSet } from 'launchdarkly-js-client-sdk';
import {
  Button,
  Heading,
  Text,
  Tooltip,
  TooltipTrigger,
  Pressable,
} from '@launchpad-ui/components';
import { Icon } from '@launchpad-ui/icons';

function App() {
  const [selectedProject, setSelectedProject] = useState<string | null>(null);
  const [sourceEnvironmentKey, setSourceEnvironmentKey] = useState<
    string | null
  >(null);
  const [flags, setFlags] = useState<LDFlagSet | null>(null);
  const [showBanner, setShowBanner] = useState(false);

  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
      }}
    >
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          minWidth: '700px',
        }}
      >
        <Box
          display="flex"
          flexDirection="column"
          alignItems="center"
          padding="1rem"
          maxWidth="1200px"
        >
          {showBanner && (
            <Box margin="0rem 0rem 2rem 0rem" width="100%">
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
              />
            </Box>
          )}
          {selectedProject && (
            <Box width="100%">
              <Flags
                selectedProject={selectedProject}
                flags={flags}
                setFlags={setFlags}
                setSourceEnvironmentKey={setSourceEnvironmentKey}
              />
            </Box>
          )}
        </Box>
      </div>
    </div>
  );
}

export default App;
