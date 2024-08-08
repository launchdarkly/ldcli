# Installation steps
1. Create a new directory:

```bash
mkdir hello-python && cd hello-python
```

2. Next, create a file called `requirements.txt` with the SDK dependency and install it:

```bash
echo "launchdarkly-server-sdk==9.4.0" >> requirements.txt && pip install -r requirements.txt
```

3. Create a file called `test.py` and add the following code:

```python
import os
import ldclient
from ldclient import Context
from ldclient.config import Config
from threading import Lock, Event


# Set sdk_key to your LaunchDarkly SDK key.
sdk_key = os.getenv("LAUNCHDARKLY_SDK_KEY")

# Set feature_flag_key to the feature flag key you want to evaluate.
feature_flag_key = "my-flag-key"


def show_evaluation_result(key: str, value: bool):
    print()
    print(f"*** The {key} feature flag evaluates to {value}")


def show_banner():
    print()
    print("        ██       ")
    print("          ██     ")
    print("      ████████   ")
    print("         ███████ ")
    print("██ LAUNCHDARKLY █")
    print("         ███████ ")
    print("      ████████   ")
    print("          ██     ")
    print("        ██       ")
    print()


class FlagValueChangeListener:
    def __init__(self):
        self.__show_banner = True
        self.__lock = Lock()

    def flag_value_change_listener(self, flag_change):
        with self.__lock:
            if self.__show_banner and flag_change.new_value:
                show_banner()
                self.__show_banner = False

            show_evaluation_result(flag_change.key, flag_change.new_value)


if __name__ == "__main__":
    if not sdk_key:
        print("*** Please set the LAUNCHDARKLY_SDK_KEY env first")
        exit()
    if not feature_flag_key:
        print("*** Please set the LAUNCHDARKLY_FLAG_KEY env first")
        exit()

    ldclient.set_config(Config(sdk_key))

    if not ldclient.get().is_initialized():
        print("*** SDK failed to initialize. Please check your internet connection and SDK credential for any typo.")
        exit()

    print("*** SDK successfully initialized")

    # Set up the evaluation context. This context should appear on your
    # LaunchDarkly contexts dashboard soon after you run the demo.
    context = \
        Context.builder('example-user-key').kind('user').name('Sandy').build()

    flag_value = ldclient.get().variation(feature_flag_key, context, False)
    show_evaluation_result(feature_flag_key, flag_value)

    change_listener = FlagValueChangeListener()
    listener = ldclient.get().flag_tracker \
        .add_flag_value_change_listener(feature_flag_key, context, change_listener.flag_value_change_listener)

    try:
        Event().wait()
    except KeyboardInterrupt:
        pass
```

Now that your application is ready, run the application to see what value we get.

```shell
LAUNCHDARKLY_SDK_KEY=YOUR_SDK_KEY python test.py
```

You should see:

`Feature flag my-flag-key is false`
