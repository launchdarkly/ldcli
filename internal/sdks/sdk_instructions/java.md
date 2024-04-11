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
  <version>7.3.0</version>
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
import com.launchdarkly.sdk.*;
import com.launchdarkly.sdk.server.*;

public class App {

  // This value is already set to your LaunchDarkly SDK key for your Test environment in the Default project.
  static final String SDK_KEY = "1234567890abcdef";

  // This value is already set to flag you just created in the Default project.
  static final String FEATURE_FLAG_KEY = "my-flag-key";

  private static void showMessage(String s) {
    System.out.println("*** " + s);
    System.out.println();
  }

  public static void main(String... args) throws Exception {
    LDConfig config = new LDConfig.Builder().build();
    final LDClient client = new LDClient(SDK_KEY, config);
    if (client.isInitialized()) {
      showMessage("SDK successfully initialized!");
    } else {
      showMessage("SDK failed to initialize");
      System.exit(1);
    }

    // Set up the evaluation context. This context should appear on your
    // LaunchDarkly contexts dashboard soon after you run the demo.
    final LDContext context = LDContext.builder("example-user-key")
        .name("Sandy")
        .build();

    // Evaluate the feature flag for this context.
    boolean flagValue = client.boolVariation(FEATURE_FLAG_KEY, context, false);
    showMessage("Feature flag '" + FEATURE_FLAG_KEY + "' is " + flagValue + " for this context");

    // Here we request that the SDK flush pending analytic events so that you see
    // data for the above evaluation on the dashboard immediatelynow rather than
    // at the next automatic flush interval. You don't need to do this under normal
    // circumstances in your own application.
    client.flush();

    // We set up a flag change listener so you can see flag changes as you change
    // the flag rules.
    client.getFlagTracker().addFlagChangeListener(event -> {
      showMessage("Feature flag '" + event.getKey() + "' has changed.");
      if (event.getKey().equals(FEATURE_FLAG_KEY)) {
        boolean value = client.boolVariation(FEATURE_FLAG_KEY, context, false);
        showMessage("Feature flag '" + FEATURE_FLAG_KEY + "' is " + value + " for this context");
      }
    });
    showMessage("Listening for feature flag changes.  Use Ctrl+C to terminate.");

    // Here we ensure that when the application terminates, the SDK shuts down
    // cleanly and has a chance to deliver analytics events to LaunchDarkly.
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
mvn clean compile assembly:single
java -jar target/hello-java-1.0-SNAPSHOT-jar-with-dependencies.jar
```

You should see:

`Feature flag my-flag-key is FALSE for this context`