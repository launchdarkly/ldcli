# Installation steps
1. Use create-vue to create a new Vue application:
```shell
npm create vue@latest
```

Enter `hello-vue` as "Project name" and hit enter at each of the other prompts to accept the defaults.

2. Change dir and install the LaunchDarkly SDK:
```shell
cd hello-vue && yarn add launchdarkly-vue-client-sdk
```

3. In `src/main.js`:
```js
import { createApp } from 'vue'
import App from './App.vue'
import { LDPlugin } from 'launchdarkly-vue-client-sdk'

const app = createApp(App)
app.use(LDPlugin, {
  clientSideID: 'myClientSideId',
  context: { kind: 'user', key: 'example-user' },
})
app.mount('#app')
```

4. In `src/App.vue`:
```html
<script setup>
import { useLDFlag, useLDReady } from 'launchdarkly-vue-client-sdk'

const ldReady = useLDReady()
const flagValue = useLDFlag('my-flag-key', false)
</script>

<template>
  <div v-if="ldReady">test-boolean-1 is {{ flagValue }}</div>
  <div v-else>LaunchDarkly client initializing...</div>
</template>
```

5. Now that your application is ready, run the application to see what value we get.
```shell
yarn dev
```

You should see:

`Feature flag my-flag-key is FALSE for this context`