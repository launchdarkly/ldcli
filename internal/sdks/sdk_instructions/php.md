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

# Create a new LDClient with your environment-specific SDK key:
$client = new LaunchDarkly\LDClient('1234567890abcdef');

# Set up the evaluation context. This context should appear on your LaunchDarkly
# contexts dashboard soon after you run the demo.
$context = LaunchDarkly\LDContext::builder("example-user-key")
  ->name("Sandy")
  ->build();

$flagValue = $client->variation("my-flag-key", $context, false);
$flagValueStr = $flagValue ? 'true' : 'false';

echo "*** Feature flag 'my-flag-key' is {$flagValueStr} for this context\n\n";
```

Now that your application is ready, run the application to see what value we get.
```shell
php index.php
```

You should see:

`Feature flag my-flag-key is FALSE`