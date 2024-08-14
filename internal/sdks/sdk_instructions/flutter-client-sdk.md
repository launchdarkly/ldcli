# Installation steps
1. Use the Flutter tool to create a new project named `hello_flutter`.
```shell
flutter create hello_flutter --platforms android,ios
```

2. Change into the directory of the created project.
```shell
cd hello_flutter
```

3. Next, add the LaunchDarkly SDK as a dependency:
```shell
flutter pub add launchdarkly_flutter_client_sdk
```

4. Ensure that `ios/Podfile` specifies a minimum deployment target of at least 10.0.
```shell
platform :ios, '10.0'
```

5. Ensure that `android/app/build.gradle` specifies a `minSdkVersion` of at least 21.
```shell
minSdkVersion 21
```

6. Open the file `lib/main.dart` and add the following code:
```dart
// Import the LaunchDarkly SDK.
import 'package:launchdarkly_flutter_client_sdk/launchdarkly_flutter_client_sdk.dart';

void main() async {
  // If initializing the SDK within a widget, this line will not be needed.
  WidgetsFlutterBinding.ensureInitialized();

  // Configure the SDK with your mobile-specific SDK key and context.
  // If building a web application the client-side ID should be used instead.
  // For a more complete example refer to:
  // https://github.com/launchdarkly/flutter-client-sdk/tree/main/packages/flutter_client_sdk/example
  final ldClient = LDClient(LDConfig('myMobileKey', AutoEnvAttributes.enabled),
      LDContextBuilder().kind('user', 'example-user-key').build());

  try {
    await ldClient.start().timeout(const Duration(seconds: 5));
    // Wait for up-to-date flags for the context, or cached flags if the
    // SDK has seen this context before.
  } catch(exception) {
    // Initialization timed out. The SDK can still be used even if
    // this times out.
  }

  // Call LaunchDarkly with the feature flag key you want to evaluate.
  final result = ldClient.boolVariation('my-flag-key', false);
  // Send analytic events to LaunchDarkly. By default events are flushed every
  // 30 seconds, and you don't need to call flush manually.
  await ldClient.flush();

  runApp(const MyApp());
}
```
Now that your application is ready, run the application to see what value we get.

Run the Android or iOS simulator and then run the application.
```shell
flutter run
```

You should see:

`Feature flag my-flag-key is FALSE for this context`
