import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App.tsx';
import { IconProvider } from './IconProvider.tsx';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <IconProvider>
      <App />
    </IconProvider>
  </React.StrictMode>,
);
