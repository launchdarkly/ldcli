# Installation steps
1. Open Xcode and create a new iOS Single View application called `hello-swift`.


2. Next, install the LaunchDarkly SDK using [CocoaPods](https://cocoapods.org/) by creating a `Podfile` and adding a dependency (you can also install with [Swift Package Manager](https://docs.launchdarkly.com/sdk/client-side/ios?site=launchDarkly#using-the-swift-package-manager), [Carthage](https://docs.launchdarkly.com/sdk/client-side/ios?site=launchDarkly#using-carthage), or [without a package manager](https://docs.launchdarkly.com/sdk/client-side/ios?site=launchDarkly#installing-the-sdk-manually)):
```
target 'hello-swift' do
pod 'LaunchDarkly', '9.6.2'
end
```

3. Install the dependencies:
```
pod install
```

You may need to turn "User Script Sandboxing" to "No" under the Project Build Settings in XCode.

4. Open `AppDelegate.swift` and add the following code:
```swift
import UIKit
// Import the LaunchDarkly SDK.
import LaunchDarkly

@UIApplicationMain
class AppDelegate: UIResponder, UIApplicationDelegate {
  var window: UIWindow?

  // Declare a variable for your mobile-specific SDK key.
  private let mobileKey = "myMobileKey"

  func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
    setUpLDClient()

    return true
  }

  // Create a function to initialize the LDClient with your mobile-specific
  // SDK key, context, and optional configurations.
  private func setUpLDClient() {
    var contextBuilder = LDContextBuilder(key: "test@email.com")
    contextBuilder.trySetValue("firstName", .string("Bob"))
    contextBuilder.trySetValue("lastName", .string("Loblaw"))
    contextBuilder.trySetValue("groups", .array([.string("beta_testers")]))

    guard case .success(let context) = contextBuilder.build()
    else { return }

    var config = LDConfig(mobileKey: mobileKey, autoEnvAttributes: .enabled)
    config.eventFlushInterval = 30.0

    LDClient.start(config: config, context: context)
  }
}
```

5. Open ViewController.swift and add the following code:
```swift
import UIKit
// Import the LaunchDarkly SDK.
import LaunchDarkly

class ViewController: UIViewController {
  // Create a variable for your flag key.
  fileprivate let featureFlagKey = "my-flag-key"

  override func viewDidLoad() {
    super.viewDidLoad()

    // Observe the LDClient for any feature flag updates.
    LDClient.get()?.observe(key: featureFlagKey, owner: self) { [weak self] changedFlag in
        self?.featureFlagDidUpdate(changedFlag.key)
    }

    checkFeatureValue()
  }

  // Create a function to call LaunchDarkly with the feature flag key you want to evaluate and print its value.
  fileprivate func checkFeatureValue() {
    if let featureFlagValue = LDClient.get() {
      let boolVal = featureFlagValue.boolVariation(forKey: featureFlagKey, defaultValue: false)
      // Ensure events are sent to LD immediately for fast completion of the Getting Started guide.
      // This line is not necessary here for production use.
      LDClient.get()?.flush()
      print("The value of (featureFlagKey) is (boolVal)")
    } else {
      print("failed to get flag, (featureFlagKey)")
    }
  }

  // Create a function to respond to flag updates.
  func featureFlagDidUpdate(_ key: LDFlagKey) {
    if key == featureFlagKey {
        checkFeatureValue()
    }
  }
}
```

Now that your application is ready, run the application through Xcode to see what value we get. You should see:

`Feature flag my-flag-key is FALSE for this context`