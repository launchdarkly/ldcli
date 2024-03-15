## Build Instructions
1. Edit `index.html` and set the value of `clientSideID` to your LaunchDarkly client-side ID. If there is an existing boolean feature flag in your LaunchDarkly project that you want to evaluate, set `flagKey` to the flag key.

```
const clientSideID = '1234567890abcdef';
const flagKey = 'my-flag-key';
```

2. Open `index.html` in your browser.

You should receive the message "Feature flag key '<flag key>' is <true/false> for this user".