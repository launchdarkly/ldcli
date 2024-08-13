import './App.css';
import { useState } from 'react';
import Flags from './Flags.tsx';
import ProjectSelector from './ProjectSelector.tsx';
import { Box } from '@launchpad-ui/core';

function App() {
  const [selectedProject, setSelectedProject] = useState<string | null>(null);

  return (
    <>
      <Box
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
        }}
      >
        <Box
          display="flex"
          flexDirection="row"
          justifyContent="space-between"
          alignItems="flex-start"
          marginBottom="2rem"
          padding="1rem"
          gap="2rem"
        >
          <ProjectSelector
            selectedProject={selectedProject}
            setSelectedProject={setSelectedProject}
          />
        </Box>
        {selectedProject != null ? (
          <Flags selectedProject={selectedProject} />
        ) : (
          ''
        )}
      </Box>
    </>
  );
}

export default App;
