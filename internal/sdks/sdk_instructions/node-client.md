# Installation steps
1. Create a new directory and create a `package.json` file:
```shell
mkdir hello-node-client && cd hello-node-client && npm init
```

2. Next, install the LaunchDarkly SDK:
```shell
npm install launchdarkly-node-client-sdk@3.1.0 --save
```

3. Create a file called index.js and add the following code:
```js
// Import the LaunchDarkly client
var LaunchDarkly = require('launchdarkly-node-client-sdk');

// Set up the user properties. This user should appear on your LaunchDarkly users dashboard
// soon after you run the demo.
var user = {
  key: "example-user-key"
};

// Create a single instance of the LaunchDarkly client
const ldClient = LaunchDarkly.initialize('1234567890abcdef', user);

function showMessage(s) {
  console.log("*** " + s);
  console.log("");
}
ldClient.waitForInitialization().then(function() {
  showMessage("SDK successfully initialized!");
  const flagValue = ldClient.variation("my-flag-key", false);

  showMessage("Feature flag " + "my-flag-key" + " is " + flagValue + ");

  // Here we ensure that the SDK shuts down cleanly and has a chance to deliver analytics
  // events to LaunchDarkly before the program exits. If analytics events are not delivered,
  // the user properties and flag usage statistics will not appear on your dashboard. In a
  // normal long-running application, the SDK would continue running and events would be
  // delivered automatically in the background.
  ldClient.close();
}).catch(function(error) {
  showMessage("SDK failed to initialize: " + error);
  process.exit(1);
});
```

In your real application, you should only call `close()` when your application is terminating -- not immediately following each `variation` call as shown in this tutorial. This is something you only need to do once.

Now that your application is ready, run the application to see what value we get.
```shell
node index.js
```

You should see:
`Feature flag my-flag-key is FALSE for this context`
