# Installation steps
If you want to skip ahead, the final code is available in our [GitHub repository](https://github.com/launchdarkly/js-core/tree/main/packages/sdk/react-native/example).

1. Use create-expo-app to create a new Expo application:
```shell
npx create-expo-app hello-react-native -t expo-template-blank-typescript && cd hello-react-native
```

2. Install the LaunchDarkly SDK:
```shell
yarn add @launchdarkly/react-native-client-sdk
```

3. In `App.tsx`:
```tsx
import {
  AutoEnvAttributes,
  LDProvider,
  ReactNativeLDClient,
} from '@launchdarkly/react-native-client-sdk';

import Welcome from './src/welcome';

const featureClient = new ReactNativeLDClient(
  'myMobileKey',
  AutoEnvAttributes.Enabled,
  {
    debug: true,
    applicationInfo: {
      id: 'ld-rn-test-app',
      version: '0.0.1',
    },
  },
);

const App = () => {
  return (
    <LDProvider client={featureClient}>
      <Welcome />
    </LDProvider>
  );
};

export default App;
```

4. Create a new file `src/welcome.tsx`:
```tsx
import { useEffect } from 'react';
import { Text, View } from 'react-native';

import { useBoolVariation, useLDClient } from '@launchdarkly/react-native-client-sdk';

export default function Welcome() {
  const flagValue = useBoolVariation('my-flag-key', false);
  const ldc = useLDClient();

  useEffect(() => {
    ldc
      .identify({ kind: 'user', key: 'example-user' })
      .catch((e: any) => console.error('error: ' + e));
  }, []);

  return (
    <View style={{ margin: 30 }}>
      <Text>my-flag-key: {flagValue.toString()}</Text>
    </View>
  );
}
```

Now that your application is ready, run the application to see what value we get.
```shell
yarn ios
```

You should see:

`Feature flag my-flag-key is FALSE for this context`

