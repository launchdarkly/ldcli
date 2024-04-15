# Installation steps
1. Open Android Studio and create a new project named `hello-android` with an empty activity.

2. Next, add the LaunchDarkly SDK as a dependency in the `app/build.gradle` file:
```java
dependencies {
  ...
  implementation("com.launchdarkly:launchdarkly-android-client-sdk:5.1.1")
}
```

3. Open the file `MainActivity.kt` and add the following code:
```java
import com.launchdarkly.sdk.android.LDClient

class MainActivity : AppCompatActivity() {

  // Set BOOLEAN_FLAG_KEY to the boolean feature flag you want to evaluate.
  val BOOLEAN_FLAG_KEY = "my-flag-key"

  override fun onCreate(savedInstanceState: Bundle?) {
      super.onCreate(savedInstanceState)
      setContentView(R.layout.activity_main)
      val textView : TextView = findViewById(R.id.textview)

      val client = LDClient.get()

      // to get the variation the SDK has cached
      textView.text = getString(
          R.string.flag_evaluated,
          BOOLEAN_FLAG_KEY,
          client.boolVariation(BOOLEAN_FLAG_KEY, false).toString()
      )

      // to register a listener to get updates in real time
      client.registerFeatureFlagListener(BOOLEAN_FLAG_KEY) {
          textView.text = getString(
              R.string.flag_evaluated,
              BOOLEAN_FLAG_KEY,
              client.boolVariation(BOOLEAN_FLAG_KEY, false).toString()
          )
      }

      // This call ensures all evaluation events show up immediately for this demo. Otherwise, the
      // SDK sends them at some point in the future. You don't need to call this in production,
      // because the SDK handles them automatically at an interval. The interval is customizable.
      client.flush()
  }
}
```

4. Add a `TextView` to your `layout/activity_main.xml` with id `textview`:
```xml
<TextView
  android:id="@+id/textview"
  android:layout_width="wrap_content"
  android:layout_height="wrap_content"
  ... />
```

5. Create `MainApplication.kt` and add the following code:
```java
import com.launchdarkly.sdk.ContextKind
import com.launchdarkly.sdk.LDContext
import com.launchdarkly.sdk.android.LDClient
import com.launchdarkly.sdk.android.LDConfig

class MainApplication : Application() {

  companion object {

      // Set LAUNCHDARKLY_MOBILE_KEY to your LaunchDarkly SDK mobile key.
      const val LAUNCHDARKLY_MOBILE_KEY = "myMobileKey"
  }

  override fun onCreate() {
      super.onCreate()

      val ldConfig = LDConfig.Builder(AutoEnvAttributes.Enabled)
          .mobileKey(LAUNCHDARKLY_MOBILE_KEY)
          .build()

      // Set up the context properties. This context should appear on your LaunchDarkly contexts
      // list soon after you run the demo.
      val context = if (isUserLoggedIn()) {
          LDContext.builder(ContextKind.DEFAULT, getUserKey())
              .name(getUserName())
              .build()
      } else {
          LDContext.builder(ContextKind.DEFAULT, "example-user-key")
              .anonymous(true)
              .build()
      }

      LDClient.init(this@MainApplication, ldConfig, context)
  }

  private fun isUserLoggedIn(): Boolean = false

  private fun getUserKey(): String = "user-key-123abc"

  private fun getUserName(): String = "Sandy"

}
```

6. Register the `MainApplication` class in the `AndroidManifest.xml`:
```xml
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
  xmlns:tools="http://schemas.android.com/tools">

  <application
    android:name=".MainApplication"
    ...
    ...
  </application>
</manifest>
```

Now that your application is ready, run the through the Android Emulator or on a real device to see what value we get. You should see:

`Feature flag my-flag-key is FALSE for this context`
