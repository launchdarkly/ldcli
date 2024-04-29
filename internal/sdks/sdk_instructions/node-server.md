# Installation steps
1. Create a new directory and create a `package.json` file:
```shell
mkdir hello-node-server && cd hello-node-server && npm init
```

2. Next, install the LaunchDarkly SDK:
```shell
npm install @launchdarkly/node-server-sdk@9.2.4 --save
```

3. Create a file called `index.js` and add the following code:
```js
// Import the LaunchDarkly client.
var LaunchDarkly = require('@launchdarkly/node-server-sdk');

// Set sdkKey to your LaunchDarkly SDK key.
const sdkKey = '1234567890abcdef';

// Set featureFlagKey to the feature flag key you want to evaluate.
const featureFlagKey = 'my-flag-key';

function showMessage(s) {
    console.log("*** " + s);
    console.log("");
}

const ldClient = LaunchDarkly.init(sdkKey);

// Set up the context properties. This context should appear on your LaunchDarkly contexts dashboard
// soon after you run the demo.
const context = {
    kind: "user",
    key: "example-context-key",
    name: "Sandy"
};

ldClient.waitForInitialization().then(function () {
    showMessage("SDK successfully initialized!");
    ldClient.variation(featureFlagKey, context, false, function (err, flagValue) {
        showMessage("Feature flag '" + featureFlagKey + "' is " + flagValue);

        // Here we ensure that the SDK shuts down cleanly and has a chance to deliver analytics
        // events to LaunchDarkly before the program exits. If analytics events are not delivered,
        // the context properties and flag usage statistics will not appear on your dashboard. In a
        // normal long-running application, the SDK would continue running and events would be
        // delivered automatically in the background.
        ldClient.flush(function () {
            ldClient.close();
        });
    });
}).catch(function (error) {
    showMessage("SDK failed to initialize: " + error);
    process.exit(1);
});
```


Now that your application is ready, run the application to see what value we get.
```shell
node index.js
```

You should see:

`Feature flag my-new-flag is FALSE for this context`
