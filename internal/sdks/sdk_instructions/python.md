# Installation steps
1. Create a new directory:

```bash
mkdir hello-python && cd hello-python
```

2. Next, create a file called `requirements.txt` with the SDK dependency and install it:

```bash
echo "launchdarkly-server-sdk==9.3.1" >> requirements.txt && pip install -r requirements.txt
```

3. Create a file called `test.py` and add the following code:

```python
# Import the LaunchDarkly client.
import ldclient
from ldclient import Context
from ldclient.config import Config

# Create a helper function for rendering messages.
def show_message(s):
    print("*** {}".format(s))
    print()

# Initialize the ldclient with your environment-specific SDK key.
if __name__ == "__main__":
    ldclient.set_config(Config("1234567890abcdef"))

# The SDK starts up the first time ldclient.get() is called.
if ldclient.get().is_initialized():
    show_message("SDK successfully initialized!")
else:
    show_message("SDK failed to initialize")
    exit()

# Set up the evaluation context. This context should appear on your LaunchDarkly contexts
# dashboard soon after you run the demo.
context = Context.builder('example-user-key').name('Sandy').build()

# Call LaunchDarkly with the feature flag key you want to evaluate.
flag_value = ldclient.get().variation("my-flag-key", context, False)

show_message("Feature flag 'my-flag-key' is {} for this user".format(flag_value))

# Here we ensure that the SDK shuts down cleanly and has a chance to deliver analytics
# events to LaunchDarkly before the program exits. If analytics events are not delivered,
# the user properties and flag usage statistics will not appear on your dashboard. In a
# normal long-running application, the SDK would continue running and events would be
# delivered automatically in the background.
ldclient.get().close()
```

Now that your application is ready, run the application to see what value we get.

```shell
python test.py
```

You should see:

`Feature flag my-flag-key is false`