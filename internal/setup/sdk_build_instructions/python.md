## Build instructions

1. Install the LaunchDarkly Python SDK by running `pip install -r requirements.txt`
2. On the command line, set the value of the environment variable `LAUNCHDARKLY_SERVER_KEY` to your LaunchDarkly SDK key.
    ```bash
    export LAUNCHDARKLY_SERVER_KEY="1234567890abcdef"
    ```
3. On the command line, set the value of the environment variable `LAUNCHDARKLY_FLAG_KEY` to an existing boolean feature flag in your LaunchDarkly project that you want to evaluate.

    ```bash
    export LAUNCHDARKLY_FLAG_KEY="my-flag-key"
    ```
4. Run `python test.py`.

You should receive the message `"Feature flag 'my-flag-key' is <true/false> for this user"`.
