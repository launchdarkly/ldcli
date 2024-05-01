# Installation steps
1. Create a new directory for your application:
```shell
mkdir hello-roku && cd hello-roku && mkdir components source
```

2. Download the latest version of the SDK [from the release page](https://github.com/launchdarkly/roku-client-sdk/releases). `PlaceLaunchDarkly.brs` in `source`, and the other two files in `components`.


3. Create a basic app `manifest` file.
```shell
title=hello-roku
major_version=1
minor_version=0
build_version=00001
```

4. Create a file with a basic scene runner named `source/main.brs` and add the following code:
```brs
sub main(params as object)
  screen = createObject("roSGScreen")
  messagePort = createObject("roMessagePort")
  screen.setMessagePort(messagePort)

  scene = screen.CreateScene("AppScene")

  screen.show()

  while (true)
      msg = wait(2500, messagePort)

      if type(msg) = "roSGScreenEvent"
          if msg.isScreenClosed() then
              return
          end if
      end if
  end while
end sub
```

5. In `components/AppScene.xml` create a basic scene by adding the following code:
```xml
<?xml version="1.0" encoding="utf-8" ?>
<component name="AppScene" extends="Scene">
    <children>
        <LaunchDarklyTask id="launchDarkly"/>

        <Label id="evaluation"
            text="waiting on payload to initialize"
            width="1280"
            height="720"
            wrap="true"
            horizAlign="center"
            vertAlign="center"
        />

        <Label id="status"
            text="waiting for sdk status report"
            width="1280"
            height="720"
            wrap="true"
            horizAlign="center"
            vertAlign="bottom"
        />

    </children>

    <script type="text/brightscript" uri="pkg:/components/AppScene.brs"/>

    <!-- Include the LaunchDarkly SDK. -->
    <script type="text/brightscript" uri="pkg:/source/LaunchDarkly.brs"/>
</component>
```

6. In `components/AppScene.brs` add the following code:
```shell
function onFeatureChange() as Void
    featureFlagKey = "my-flag-key"

    value = m.ld.variation(featureFlagKey, false)

    if value then
      m.top.backgroundColor = &h00844BFF
      m.evaluation.text = "The " + featureFlagKey + " feature flag evaluates to true"
    else
      m.top.backgroundColor = &h373841FF
      m.evaluation.text = "The " + featureFlagKey + " feature flag evaluates to false"
    end if
end function

function onStatusChange() as Void
    if m.ld.status.getStatus() = m.ld.status.map.initialized
      m.status.text = "SDK successfully initialized"
    else
      m.status.text = "SDK failed to initialize. Please check your internet connection and SDK credential for any typo."
    end if
end function

function init() as Void
    mobileKey = "myMobileKey"

    launchDarklyNode = m.top.findNode("launchDarkly")
    launchDarklyNode.observeField("flags", "onFeatureChange")
    launchDarklyNode.observeField("status", "onStatusChange")

    config = LaunchDarklyConfig(mobileKey, launchDarklyNode)

    ' Set up the user-kind context properties. This context should appear on
    ' your LaunchDarkly contexts dashboard soon after you run the demo.
    context = LaunchDarklyCreateContext({kind: "user", key: "example-user-key", name: "Sandy"})
    LaunchDarklySGInit(config, context)

    m.ld = LaunchDarklySG(launchDarklyNode)

    m.evaluation = m.top.findNode("evaluation")
    m.evaluation.font.size=40
    m.evaluation.color="0xFFFFFFFF"

    m.status = m.top.findNode("status")
    m.status.font.size=20
    m.status.color="0xFFFFFFFF"

    m.top.backgroundColor = &h373841FF
    m.top.backgroundUri = ""

    onFeatureChange()
end function
```

Upload your code to your Roku, telnet in, and check the results. Follow these build instructions:

Create a deployable package with `zip -r app.zip .`
Upload `app.zip` to your device

You should see:

`Feature flag my-flag-key is FALSE`
