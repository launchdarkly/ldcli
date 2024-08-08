# Installation steps
1. Create a file called `index.html` and add the following code:
```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta http-equiv="X-UA-Compatible" content="IE=edge;chrome=1">
    <title>LaunchDarkly tutorial</title>
    <script src="https://unpkg.com/launchdarkly-js-client-sdk@3"></script>
  </head>
  <body style="margin: 0;
    background: #373841;
    color: white;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen',
      'Ubuntu', 'Cantarell', 'Fira Sans', 'Droid Sans', 'Helvetica Neue',
      sans-serif;
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
    text-align: center">
    <script>
    function main()
    {
      // Set clientSideID to your LaunchDarkly client-side ID
      const clientSideID = 'myClientSideId';

      // Set flagKey to the feature flag key you want to evaluate
      const flagKey = 'my-flag-key';

      // Set up the evaluation context. This context should appear on your
      // LaunchDarkly contexts dashboard soon after you run the demo.
      const context = {
        kind: 'user',
        key: 'example-context-key',
        name: 'Sandy'
      };

      var div = document.createElement('div');
      document.body.appendChild(div);
      div.appendChild(document.createTextNode('Initializing...'));

      const ldclient = LDClient.initialize(clientSideID, context);

      function render() {
        const flagValue = ldclient.variation(flagKey, false);
        const label = 'The ' + flagKey + ' feature flag evaluates to ' + flagValue + '.';
        document.body.style.background = flagValue ? '#00844B' : '#373841';
        div.replaceChild(document.createTextNode(label), div.firstChild);
      }

      ldclient.on('initialized', () => {
        div.replaceChild(document.createTextNode('SDK successfully initialized!'), div.firstChild);
      });
      ldclient.on('failed', () => {
        div.replaceChild(document.createTextNode('SDK failed to initialize'), div.firstChild);
      });
      ldclient.on('ready', render);
      ldclient.on('change', render);

    }
    main();
    </script>
  </body>
</html>
```
Now that your application is ready, run the application to see what value we get.

Open `index.html` in your browser. You should see:

`Feature flag my-flag-key is FALSE for this context`
