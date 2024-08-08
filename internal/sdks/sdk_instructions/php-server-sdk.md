# Installation steps
1. Create a new directory and install [Composer](https://getcomposer.org/):
```shell
mkdir hello-php && cd hello-php && curl -sS https://getcomposer.org/installer | php
```

2. Next, install the LaunchDarkly SDK and Guzzle dependency:
```shell
php composer.phar require launchdarkly/server-sdk:6.1.0 guzzlehttp/guzzle
```

3. Create a file called `index.php` and add the following code:
```php
<?php

require 'vendor/autoload.php';

function showEvaluationResult(string $key, bool $value) {
  echo PHP_EOL;
  echo sprintf("*** %%s: The %%s feature flag evaluates to %%s", date("h:i:s"), $key, $value ? 'true' : 'false');
  echo PHP_EOL;
}

function showBanner() {
  echo PHP_EOL;
  echo "        ██       " . PHP_EOL;
  echo "          ██     " . PHP_EOL;
  echo "      ████████   " . PHP_EOL;
  echo "         ███████ " . PHP_EOL;
  echo "██ LAUNCHDARKLY █" . PHP_EOL;
  echo "         ███████ " . PHP_EOL;
  echo "      ████████   " . PHP_EOL;
  echo "          ██     " . PHP_EOL;
  echo "        ██       " . PHP_EOL;
  echo PHP_EOL;
}

// Set $sdkKey to your LaunchDarkly SDK key.
$sdkKey = getenv("LAUNCHDARKLY_SDK_KEY") ?? "YOUR_SDK_KEY";

// Set $featureFlagKey to the feature flag key you want to evaluate.
$featureFlagKey = "my-flag-key";


if (!$sdkKey) {
echo "*** Please set the environment variable LAUNCHDARKLY_SDK_KEY to your LaunchDarkly SDK key first" . PHP_EOL . PHP_EOL;
exit(1);
} else if (!$featureFlagKey) {
echo "*** Please set the environment variable LAUNCHDARKLY_FLAG_KEY to a boolean flag first" . PHP_EOL . PHP_EOL;
exit(1);
}

$client = new LaunchDarkly\LDClient($sdkKey);

// Set up the evaluation context. This context should appear on your LaunchDarkly contexts dashboard soon after you run the demo.
$context = LaunchDarkly\LDContext::builder("example-user-key")
->kind("user")
->name("Sandy")
->build();


$showBanner = true;
$lastValue = null;
do {
  $flagValue = $client->variation($featureFlagKey, $context, false);

  if ($flagValue !== $lastValue) {
      showEvaluationResult($featureFlagKey, $flagValue);
  }

  if ($showBanner && $flagValue) {
      showBanner();
      $showBanner = false;
  }

  $lastValue = $flagValue;
  sleep(1);
} while(true);
```

Now that your application is ready, run the application to see what value we get.
```shell
LAUNCHDARKLY_SDK_KEY=YOUR_SDK_KEY php index.php
```

You should see:

`Feature flag my-flag-key is FALSE`
