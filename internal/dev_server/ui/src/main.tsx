import React, { useEffect } from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router';
import App from './App.tsx';
import { IconProvider } from './IconProvider.tsx';
import { ToastContainer } from '@launchpad-ui/components';

const Root = () => {
  useEffect(() => {
    const darkModeMediaQuery = window.matchMedia(
      '(prefers-color-scheme: dark)',
    );
    const handleChange = (e: MediaQueryListEvent) => {
      document.documentElement.setAttribute(
        'data-theme',
        e.matches ? 'dark' : 'default',
      );
    };

    // Idk why but typescript is not happy with the type of darkModeMediaQuery
    handleChange(darkModeMediaQuery as unknown as MediaQueryListEvent);
    darkModeMediaQuery.addEventListener('change', handleChange);

    return () => {
      darkModeMediaQuery.removeEventListener('change', handleChange);
    };
  }, []);

  return (
    <React.StrictMode>
      <BrowserRouter>
        <IconProvider>
          <App />
          <ToastContainer />
        </IconProvider>
      </BrowserRouter>
    </React.StrictMode>
  );
};

ReactDOM.createRoot(document.getElementById('root')!).render(<Root />);
