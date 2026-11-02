import './App.css';
import { useEffect } from 'react';
import { Routes, Route, Navigate, useLocation } from 'react-router';
import { pageNameFromPath, sendUiAnalytics } from './uiAnalytics';
import { Box } from '@launchpad-ui/core';
import RouteSelector from './RouteSelector.tsx';
import FlagsPage from './FlagsPage.tsx';
import EventsPage from './EventsPage.tsx';
import DebugSessionsPage from './DebugSessionsPage.tsx';
import DebugSessionEventsPage from './DebugSessionEventsPage.tsx';

export function PageAnalytics() {
  const location = useLocation();

  useEffect(() => {
    const page = pageNameFromPath(location.pathname);
    if (page) {
      sendUiAnalytics('Dev Server UI Page Viewed', { page });
    }
  }, [location.pathname]);

  return null;
}

function App() {
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
        <PageAnalytics />
        <Box display="flex" justifyContent="flex-start" width="100%">
          <RouteSelector />
        </Box>
        <Box padding="1rem" width="100%">
          <Routes>
            <Route path="/" element={<Navigate to="/ui/flags" replace />} />
            <Route path="/ui" element={<Navigate to="/ui/flags" replace />} />
            <Route path="/ui/flags" element={<FlagsPage />} />
            <Route path="/ui/events" element={<EventsPage />} />
            <Route path="/ui/debug-sessions" element={<DebugSessionsPage />} />
            <Route
              path="/ui/debug-sessions/:debugSessionKey/events"
              element={<DebugSessionEventsPage />}
            />
          </Routes>
        </Box>
      </Box>
    </div>
  );
}

export default App;
