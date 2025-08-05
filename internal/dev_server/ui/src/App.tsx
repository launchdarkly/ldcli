import './App.css';
import { useState } from 'react';
import { Box } from '@launchpad-ui/core';
import FlagsButton from './FlagsButton.tsx';
import EventsButton from './EventsButton.tsx';
import FlagsPage from './FlagsPage.tsx';
import EventsPage from './EventsPage.tsx';

function App() {
  const [mode, setMode] = useState<'flags' | 'events'>('flags');

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
        display="flex"
        flexDirection="column"
        alignItems="center"
        width="100%"
        maxWidth="900px"
        minWidth="600px"
        padding="2rem"
      >
        <Box display="flex" gap="10px" justifyContent="flex-start" width="100%">
          <FlagsButton onPress={() => { setMode('flags'); }} />
          <EventsButton onPress={() => { setMode('events'); }} />
        </Box>
        <Box padding="1rem" width="100%">
          {mode === 'flags' && <FlagsPage />}
          {mode === 'events' && <EventsPage />}
        </Box>
      </Box>
    </div>
  );
}


export default App;
