# Installation steps
1. Create a new directory for your application:
```shell
mkdir hello-go && cd hello-go
```

2. Start your module using the go mod init command:
```shell
go mod init example/hello-go
```

3. Next, install the SDK (SDK v6 requires go 1.18+):
```shell
go get github.com/launchdarkly/go-server-sdk/v6
```

4. Create a file called main.go and add the following code:
```go
package main
  import (
    "fmt"
    "os"
    "time"

    "github.com/launchdarkly/go-sdk-common/v3/ldcontext"
    ld "github.com/launchdarkly/go-server-sdk/v6"
  )

  // Set sdkKey to your LaunchDarkly SDK key.
  const sdkKey = "1234567890abcdef"

  // Set featureFlagKey to the feature flag key you want to evaluate.
  const featureFlagKey = "my-flag-key"

  func showMessage(s string) { fmt.Printf("*** %%s\n\n", s) }

  func main() {
    ldClient, _ := ld.MakeClient(sdkKey, 5*time.Second)
    if ldClient.Initialized() {
      showMessage("SDK successfully initialized!")
    } else {
      showMessage("SDK failed to initialize")
      os.Exit(1)
    }

    // Set up the evaluation context. This context should appear on your LaunchDarkly contexts dashboard
    // soon after you run the demo.
    context := ldcontext.NewBuilder("example-user-key").
      Name("Sandy").
      Build()

    flagValue, err := ldClient.BoolVariation(featureFlagKey, context, false)
    if err != nil {
      showMessage("error: " + err.Error())
    }

    showMessage(fmt.Sprintf("Feature flag '%%s' is %%t for this context", featureFlagKey, flagValue))

    // Here we ensure that the SDK shuts down cleanly and has a chance to deliver analytics
    // events to LaunchDarkly before the program exits. If analytics events are not delivered,
    // the context attributes and flag usage statistics will not appear on your dashboard. In
    // a normal long-running application, the SDK would continue running and events would be
    // delivered automatically in the background.
    ldClient.Close()
  }
```

Now that your application is ready, run the application to see what value we get.
```shell
go build && ./hello-go
```

You should see:

`Feature flag my-flag-key is FALSE for this context`