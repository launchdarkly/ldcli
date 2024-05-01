# Installation steps
1. Open Android Studio and create a new project named `hello-android` with an empty activity.

2. Next, add the LaunchDarkly SDK as a dependency in the `app/build.gradle` file:
```java
dependencies {
  ...
  implementation("com.launchdarkly:launchdarkly-android-client-sdk:5.2.0")
}
```

3. Open the file `MainActivity.kt` and add the following code:
```java
package com.launchdarkly.hello_android

import android.os.Bundle
import android.view.View
import android.widget.TextView
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import com.launchdarkly.hello_android.MainApplication.Companion.LAUNCHDARKLY_MOBILE_KEY
import com.launchdarkly.sdk.android.LDClient

class MainActivity : AppCompatActivity() {

    // Set BOOLEAN_FLAG_KEY to the feature flag key you want to evaluate.
    val BOOLEAN_FLAG_KEY = "my-flag-key"

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        val textView : TextView = findViewById(R.id.textview)
        val fullView : View = window.decorView

        if (LAUNCHDARKLY_MOBILE_KEY == "example-mobile-key") {
            val builder = AlertDialog.Builder(this)
            builder.setMessage("LAUNCHDARKLY_MOBILE_KEY was not customized for this application.")
            builder.create().show()
        }

        val client = LDClient.get()
        val flagValue = client.boolVariation(BOOLEAN_FLAG_KEY, false)

        // to get the variation the SDK has cached
        textView.text = getString(
            R.string.flag_evaluated,
            BOOLEAN_FLAG_KEY,
            flagValue.toString()
        )

        // Style the display
        textView.setTextColor(resources.getColor(R.color.colorText))
        if(flagValue) {
            fullView.setBackgroundColor(resources.getColor(R.color.colorBackgroundTrue))
        } else {
            fullView.setBackgroundColor(resources.getColor(R.color.colorBackgroundFalse))
        }

        // to register a listener to get updates in real time
        client.registerFeatureFlagListener(BOOLEAN_FLAG_KEY) {
            val changedFlagValue = client.boolVariation(BOOLEAN_FLAG_KEY, false)
            textView.text = getString(
                R.string.flag_evaluated,
                BOOLEAN_FLAG_KEY,
                changedFlagValue.toString()
            )
            if(changedFlagValue) {
                fullView.setBackgroundColor(resources.getColor(R.color.colorBackgroundTrue))
            } else {
                fullView.setBackgroundColor(resources.getColor(R.color.colorBackgroundFalse))
            }
        }
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
package com.launchdarkly.hello_android

import android.app.Application
import com.launchdarkly.sdk.ContextKind
import com.launchdarkly.sdk.LDContext
import com.launchdarkly.sdk.android.LDClient
import com.launchdarkly.sdk.android.LDConfig
import com.launchdarkly.sdk.android.LDConfig.Builder.AutoEnvAttributes

class MainApplication : Application() {

    companion object {

        // Set LAUNCHDARKLY_MOBILE_KEY to your LaunchDarkly SDK mobile key.
        const val LAUNCHDARKLY_MOBILE_KEY = "myMobileKey"
    }

    override fun onCreate() {
        super.onCreate()

        // Set LAUNCHDARKLY_MOBILE_KEY to your LaunchDarkly mobile key found on the LaunchDarkly
        // dashboard in the start guide.
        // If you want to disable the Auto EnvironmentAttributes functionality.
        // Use AutoEnvAttributes.Disabled as the argument to the Builder
        val ldConfig = LDConfig.Builder(AutoEnvAttributes.Enabled)
            .mobileKey(LAUNCHDARKLY_MOBILE_KEY)
            .build()

        // Set up the context properties. This context should appear on your LaunchDarkly context
        // dashboard soon after you run the demo.
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

    private fun getUserKey(): String = "example-user-key"

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
