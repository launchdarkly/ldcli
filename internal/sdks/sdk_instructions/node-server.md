# Installation steps
1. Create a new directory and create a `package.json` file:
```shell
mkdir hello-node-server && cd hello-node-server && npm init
```

2. Next, install the LaunchDarkly SDK:
```shell
npm install @launchdarkly/node-server-sdk@9.4.1 --save
```

3. Create a file called `index.js` and add the following code:
```js
const LaunchDarkly = require('@launchdarkly/node-server-sdk');

// Set sdkKey to your LaunchDarkly SDK key.
const sdkKey = process.env.LAUNCHDARKLY_SDK_KEY ?? 'YOUR_SDK_KEY';

// Set featureFlagKey to the feature flag key you want to evaluate.
const featureFlagKey = 'my-flag-key';

function showBanner() {
  console.log(
    `      ██
          ██
      ████████
         ███████
██ LAUNCHDARKLY █
         ███████
      ████████
          ██
        ██
`,
  );
}

function printValueAndBanner(flagValue) {
  console.log(`*** The '${featureFlagKey}' feature flag evaluates to ${flagValue}.`);

  if (flagValue) showBanner();
}

if (!sdkKey) {
  console.log('*** Please edit index.js to set sdkKey to your LaunchDarkly SDK key first.');
  process.exit(1);
}

const ldClient = LaunchDarkly.init(sdkKey);

// Set up the context properties. This context should appear on your LaunchDarkly contexts dashboard
// soon after you run the demo.
const context = {
  kind: 'user',
  key: 'example-user-key',
  name: 'Sandy',
};

ldClient
  .waitForInitialization()
  .then(() => {
    console.log('*** SDK successfully initialized!');

    const eventKey = `update:${featureFlagKey}`;
    ldClient.on(eventKey, () => {
      ldClient.variation(featureFlagKey, context, false).then(printValueAndBanner);
    });

    ldClient.variation(featureFlagKey, context, false).then((flagValue) => {
      printValueAndBanner(flagValue);

      if(typeof process.env.CI !== "undefined") {
        process.exit(0);
      }
    });
  })
  .catch((error) => {
    console.log(`*** SDK failed to initialize: ${error}`);
    process.exit(1);
  });
```

Now that your application is ready, run the application to see what value we get.
```shell
LAUNCHDARKLY_SDK_KEY=YOUR_SDK_KEY node index.js
```

You should see:

`Feature flag my-new-flag is FALSE for this context`
