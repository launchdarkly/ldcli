# Installation steps
If you want to skip ahead, the final code is available in our [GitHub repository](https://github.com/launchdarkly/hello-roku).

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
    <!-- Include the LaunchDarklyTask. -->
    <LaunchDarklyTask id="launchDarkly"/>
  </children>

  <script type="text/brightscript" uri="pkg:/components/AppScene.brs"/>

  <!-- Include the LaunchDarkly SDK. -->
  <script type="text/brightscript" uri="pkg:/source/LaunchDarkly.brs"/>
</component>
```

6. In `components/AppScene.brs` add the following code:
```shell
' Create a function to handle flag changes.
function onStatusChange() as Void
  if m.ld.status.getStatus() = m.ld.status.map.initialized then
    print "evaluation: " m.ld.variation("my-flag-key", false)
    ' Ensure events are sent to LD immediately for fast completion of the Getting Started guide.
    ' This line is not necessary here for production use.
    m.ld.flush()
  else
    print "SDK status changed to " m.ld.status.getStatusAsString()
  end if
end function

' Initialize the LaunchDarkly SDK and configure it with your LaunchDarkly
' environment-specific SDK key and user.
function init() as Void
  launchDarklyNode = m.top.findNode("launchDarkly")

  config = LaunchDarklyConfig("1234567890abcdef", launchDarklyNode)
  config.setLogLevel(LaunchDarklyLogLevels().debug)

  context = LaunchDarklyCreateContext({kind: "user", key: "example-user-key"})

  LaunchDarklySGInit(config, context)

  m.ld = LaunchDarklySG(launchDarklyNode)

  launchDarklyNode.observeField("status", "onStatusChange")
end function
```

Upload your code to your Roku, telnet in, and check the results. Follow these build instructions:

Create a deployable package with `zip -r app.zip .`
Upload `app.zip` to your device

You should see:

`Feature flag my-flag-key is FALSE`