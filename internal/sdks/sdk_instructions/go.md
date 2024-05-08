# Installation steps
1. Create a new directory for your application:
```shell
mkdir hello-go && cd hello-go
```

2. Start your module using the go mod init command:
```shell
go mod init example/hello-go
```

3. Next, install the SDK (SDK v7 requires go 1.18+):
```shell
go get github.com/launchdarkly/go-server-sdk/v7
```

4. Create a file called main.go and add the following code:
```go
package main

import (
  "fmt"
  "os"
  "time"

  "github.com/launchdarkly/go-sdk-common/v3/ldcontext"
  "github.com/launchdarkly/go-sdk-common/v3/ldvalue"
  ld "github.com/launchdarkly/go-server-sdk/v7"
)

func showBanner() {
  fmt.Print("\n        ██       \n" +
    "          ██     \n" +
    "      ████████   \n" +
    "         ███████ \n" +
    "██ LAUNCHDARKLY █\n" +
    "         ███████ \n" +
    "      ████████   \n" +
    "          ██     \n" +
    "        ██       \n")
}

func showMessage(s string) { fmt.Printf("*** %%s\n\n", s) }

func main() {
  var sdkKey = os.Getenv("LAUNCHDARKLY_SDK_KEY")

  if sdkKey == "" {
    showMessage("LaunchDarkly SDK key is required: set the LAUNCHDARKLY_SDK_KEY environment variable and try again.")
    os.Exit(1)
  }

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

  // Set featureFlagKey to the feature flag key you want to evaluate.
  var featureFlagKey = "my-flag-key"

  if os.Getenv("LAUNCHDARKLY_FLAG_KEY") != "" {
    featureFlagKey = os.Getenv("LAUNCHDARKLY_FLAG_KEY")
  }

  flagValue, err := ldClient.BoolVariation(featureFlagKey, context, false)
  if err != nil {
    showMessage("error: " + err.Error())
  }

  showMessage(fmt.Sprintf("The '%%s' feature flag evaluates to %%t.", featureFlagKey, flagValue))

  if flagValue {
    showBanner()
  }

  if os.Getenv("CI") != "" {
    os.Exit(0)
  }

  updateCh := ldClient.GetFlagTracker().AddFlagValueChangeListener(featureFlagKey, context, ldvalue.Null())

  for event := range updateCh {
    showMessage(fmt.Sprintf("The '%%s' feature flag evaluates to %%t.", featureFlagKey, event.NewValue.BoolValue()))
    if event.NewValue.BoolValue() {
      showBanner()
    }
  }
}
```

Now that your application is ready, run the application to see what value we get.
```shell
export LAUNCHDARKLY_SDK_KEY=1234567890abcdef
go build && ./hello-go
```

You should see:

`*** The 'my-flag-key' feature flag evaluates to false.`
