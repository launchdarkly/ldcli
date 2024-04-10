# Installation steps
If you want to skip ahead, the final code is available in our [GitHub repository](https://github.com/launchdarkly/react-client-sdk/tree/main/examples/typescript).

1. Use create-react-app to create a new React application:

```shell
npx create-react-app hello-react --template typescript && cd hello-react
```

2. Install the LaunchDarkly SDK:

```shell
npm install --save launchdarkly-react-client-sdk@3.1.0
```

3. In `index.tsx`:

```tsx
import React from 'react';
import ReactDOM from 'react-dom/client';
import './index.css';
import App from './App';
import { asyncWithLDProvider } from 'launchdarkly-react-client-sdk';

(async () => {
    const LDProvider = await asyncWithLDProvider({
        clientSideID: 'myClientSideId',
        context: {
            kind: 'user',
            key: 'example-user',
            name: 'Example user',
        },
    });

    const root = ReactDOM.createRoot(document.getElementById('root') as HTMLElement);
    root.render(
        <React.StrictMode>
            <LDProvider>
                <App />
            </LDProvider>
        </React.StrictMode>,
    );
})();
```

4. In `App.tsx`:

```tsx
import './App.css';
import { useFlags } from 'launchdarkly-react-client-sdk';

function App() {
    const { myFlagKey } = useFlags();

    return (
        <div className="App">
            <header className="App-header">
                <p>{myFlagKey ? <b>Flag on</b> : <b>Flag off</b>}</p>
            </header>
        </div>
    );
}

export default App;
```

Note that `my-flag-key` is accessed in camel-cased form. Read [our documentation](https://docs.launchdarkly.com/sdk/client-side/react/react-web?site=launchDarkly#flag-keys) to learn more about referencing flag keys in React.

Now that your application is ready, run the application to see what value we get.

```shell
npm start
```

You should see:

`Feature flag my-flag-key is FALSE for this context`