import React, { useEffect } from 'react';
import ReactDOM from 'react-dom/client';
import App from './App.tsx';
import { IconProvider } from './IconProvider.tsx';

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
      <IconProvider>
        <App />
      </IconProvider>
    </React.StrictMode>
  );
};

ReactDOM.createRoot(document.getElementById('root')!).render(<Root />);
