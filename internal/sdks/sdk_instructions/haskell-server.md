# Installation steps
1. Create a new project for your application:
```shell
stack new hello-haskell && cd hello-haskell
```

2. Next, add the SDK and text package to your list of dependencies in package.yaml:
```shell
launchdarkly-server-sdk, text
```

3. Add the SDK version as an `extra-deps` entry in `stack.yaml`:
```shell
- launchdarkly-server-sdk-4.1.0
```

4. Edit `app/Main.hs` by adding the following code:
```haskell
{-# LANGUAGE OverloadedStrings #-}

module Main where

-- Import helper libraries.
import Control.Concurrent  (threadDelay)
import Control.Monad       (forever)

-- Import the LaunchDarkly SDK.
import LaunchDarkly.Server

-- Define main method
main :: IO ()
main = do
  -- Create a new LDClient instance with your environment-specific SDK Key.
  client <- makeClient $ makeConfig "1234567890abcdef"

  -- Set up the context properties. This context should appear on your LaunchDarkly contexts dashboard
  -- soon after you run the demo.
  let context = makeContext "example-user-key" "user"

  forever $ do
    -- Call LaunchDarkly with the feature flag key you want to evaluate.
    launched <- boolVariation client "my-flag-key" context False
    -- Ensure events are sent to LD immediately for fast completion of the Getting Started guide.
    -- This line is not necessary here for production use.
    flushEvents client
    putStrLn $ "Flag is: " ++ show launched
    -- one second
    threadDelay $ 1 * 1000000
```

Now that your application is ready, run the application to see what value we get.
```shell
stack build && stack exec hello-haskell-exe
```

You should see:

`Feature flag my-flag-key is FALSE`