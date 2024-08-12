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

    // Set sdkKey to your LaunchDarkly mobile key.
    private let sdkKey = "myMobileKey"

    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        setUpLDClient()

        return true
    }

    private func setUpLDClient() {
        // Set up the evaluation context. This context should appear on your
        // LaunchDarkly contexts dashboard soon after you run the demo.
        var contextBuilder = LDContextBuilder(key: "example-user-key")
        contextBuilder.kind("user")
        contextBuilder.name("Sandy")

        guard case .success(let context) = contextBuilder.build()
        else { return }

        let config = LDConfig(mobileKey: sdkKey, autoEnvAttributes: .enabled)
        LDClient.start(config: config, context: context, startWaitSeconds: 30)
    }
}
```

5. Open ViewController.swift and add the following code:
```swift
import UIKit
import LaunchDarkly

class ViewController: UIViewController {

    @IBOutlet weak var featureFlagLabel: UILabel!

    // Set featureFlagKey to the feature flag key you want to evaluate.
    fileprivate let featureFlagKey = "my-flag-key"

    override func viewDidLoad() {
        super.viewDidLoad()

        if let ld = LDClient.get() {
            ld.observe(key: featureFlagKey, owner: self) { [weak self] changedFlag in
                guard let me = self else { return }
                guard case .bool(let booleanValue) = changedFlag.newValue else { return }

                me.updateUi(flagKey: changedFlag.key, result: booleanValue)
            }
            let result = ld.boolVariation(forKey: featureFlagKey, defaultValue: false)
            updateUi(flagKey: featureFlagKey, result: result)
        }
    }

    func updateUi(flagKey: String, result: Bool) {
        self.featureFlagLabel.text = "The (flagKey) feature flag evaluates to (result)"

        let toggleOn = UIColor(red: 0, green: 0.52, blue: 0.29, alpha: 1)
        let toggleOff = UIColor(red: 0.22, green: 0.22, blue: 0.25, alpha: 1)
        self.view.backgroundColor = result ? toggleOn : toggleOff
    }
}
```

Now that your application is ready, run the application through Xcode to see what value we get. You should see:

`Feature flag my-flag-key is FALSE for this context`
