# Installation steps
1. Create a new project and accept the default options suggested by maven:
```shell
mvn archetype:generate -DgroupId=com.launchdarkly.tutorial -DartifactId=hello-java
```

2. Change into the project directory:
```shell
cd hello-java
```

3. Add the SDK to your project in your `pom.xml <dependencies>` section:
```xml
<dependency>
  <groupId>com.launchdarkly</groupId>
  <artifactId>launchdarkly-java-server-sdk</artifactId>
  <version>7.4.0</version>
</dependency>
```

4. Configure the Maven Assembly Plugin in your `pom.xml` to make it easier to run the application:
```xml
<build>
  <plugins>
    <plugin>
      <artifactId>maven-assembly-plugin</artifactId>
      <configuration>
        <archive>
          <manifest>
            <mainClass>com.launchdarkly.tutorial.App</mainClass>
          </manifest>
        </archive>
        <descriptorRefs>
          <descriptorRef>jar-with-dependencies</descriptorRef>
        </descriptorRefs>
      </configuration>
    </plugin>
  </plugins>
</build>
```

5. Depending on your java version, you may need to change the compilation source and target level in `pom.xml`:
```xml
<maven.compiler.source>1.8</maven.compiler.source>
    <maven.compiler.target>1.8</maven.compiler.target>
```

6. Add the following code to `App.java`:
```java
import java.io.IOException;

import com.launchdarkly.sdk.*;
import com.launchdarkly.sdk.server.*;

public class App {

  // Set SDK_KEY to your LaunchDarkly SDK key.
  static String SDK_KEY = "YOUR_SDK_KEY";

  // Set FEATURE_FLAG_KEY to the feature flag key you want to evaluate.
  static String FEATURE_FLAG_KEY = "my-flag-key";

  private static void showMessage(String s) {
    System.out.println("*** " + s);
    System.out.println();
  }

  private static void showBanner() {
    showMessage("\n        ██       \n" +
                "          ██     \n" +
                "      ████████   \n" +
                "         ███████ \n" +
                "██ LAUNCHDARKLY █\n" +
                "         ███████ \n" +
                "      ████████   \n" +
                "          ██     \n" +
                "        ██       \n");
  }

  public static void main(String... args) throws Exception {
    boolean CIMode = System.getenv("CI") != null;

    String envSDKKey = System.getenv("LAUNCHDARKLY_SDK_KEY");
    if(envSDKKey != null) {
      SDK_KEY = envSDKKey;
    }

    String envFlagKey = System.getenv("LAUNCHDARKLY_FLAG_KEY");
    if(envFlagKey != null) {
      FEATURE_FLAG_KEY = envFlagKey;
    }

    LDConfig config = new LDConfig.Builder().build();

    if (SDK_KEY == null || SDK_KEY.equals("")) {
      showMessage("Please set the LAUNCHDARKLY_SDK_KEY environment variable or edit Hello.java to set SDK_KEY to your LaunchDarkly SDK key first.");
      System.exit(1);
    }

    final LDClient client = new LDClient(SDK_KEY, config);
    if (client.isInitialized()) {
      showMessage("SDK successfully initialized!");
    } else {
      showMessage("SDK failed to initialize.  Please check your internet connection and SDK credential for any typo.");
      System.exit(1);
    }

    // Set up the evaluation context. This context should appear on your
    // LaunchDarkly contexts dashboard soon after you run the demo.
    final LDContext context = LDContext.builder("example-user-key")
        .name("Sandy")
        .build();

    // Evaluate the feature flag for this context.
    boolean flagValue = client.boolVariation(FEATURE_FLAG_KEY, context, false);
    showMessage("The '" + FEATURE_FLAG_KEY + "' feature flag evaluates to " + flagValue + ".");

    if (flagValue) {
      showBanner();
    }

    //If this is building for CI, we don't need to keep running the Hello App continously.
    if(CIMode) {
      System.exit(0);
    }

    // We set up a flag change listener so you can see flag changes as you change
    // the flag rules.
    client.getFlagTracker().addFlagValueChangeListener(FEATURE_FLAG_KEY, context, event -> {
        showMessage("The '" + FEATURE_FLAG_KEY + "' feature flag evaluates to " + event.getNewValue() + ".");

        if (event.getNewValue().booleanValue()) {
          showBanner();
        }
    });
    showMessage("Listening for feature flag changes.");

    // Here we ensure that when the application terminates, the SDK shuts down
    // cleanly and has a chance to deliver analytics events to LaunchDarkly.
    // If analytics events are not delivered, the context attributes and flag usage
    // statistics may not appear on your dashboard. In a normal long-running
    // application, the SDK would continue running and events would be delivered
    // automatically in the background.
    Runtime.getRuntime().addShutdownHook(new Thread(new Runnable() {
      public void run() {
        try {
          client.close();
        } catch (IOException e) {
          // ignore
        }
      }
    }, "ldclient-cleanup-thread"));

    // Keeps example application alive.
    Object mon = new Object();
    synchronized (mon) {
      mon.wait();
    }
  }
}
```

Now that your application is ready, run the application to see what value we get.
```shell
mvn clean compile assembly:single && LAUNCHDARKLY_SDK_KEY=YOUR_SDK_KEY java -jar target/hello-java-1.0-SNAPSHOT-jar-with-dependencies.jar
```

You should see:

`Feature flag my-flag-key is FALSE for this context`
