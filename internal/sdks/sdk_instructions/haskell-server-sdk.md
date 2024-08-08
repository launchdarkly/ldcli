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
{-# LANGUAGE OverloadedStrings, NumericUnderscores #-}
module Main where
import Control.Concurrent  (threadDelay)
import Control.Monad       (forever)
import Data.Text           (Text, pack)
import Data.Function       ((&))
import qualified LaunchDarkly.Server as LD
import System.Timeout (timeout)
import Text.Printf (printf, hPrintf)
import System.Environment (lookupEnv)

showEvaluationResult :: String -> Bool -> IO ()
showEvaluationResult key value = do
    printf "*** The %%s feature flag evaluates to %%s\n" key (show value)

showBanner :: IO ()
showBanner = putStr "\n\
\        ██       \n\
\          ██     \n\
\      ████████   \n\
\         ███████ \n\
\██ LAUNCHDARKLY █\n\
\         ███████ \n\
\      ████████   \n\
\          ██     \n\
\        ██       \n\
\\n\
\"

showMessage :: String -> Bool -> Maybe Bool -> Bool -> IO Bool
showMessage key True _ True = do
    showBanner
    showEvaluationResult key True
    pure False
showMessage key value Nothing showBanner = do
    showEvaluationResult key value
    pure showBanner
showMessage key value (Just lastValue) showBanner
    | value /= lastValue = do
        showEvaluationResult key value
        pure showBanner
    | otherwise = pure showBanner

waitForClient :: LD.Client -> IO Bool
waitForClient client = do
    status <- LD.getStatus client
    case status of
        LD.Uninitialized -> threadDelay (1 * 1_000) >> waitForClient client
        LD.Initialized -> return True
        _anyOtherStatus -> return False

evaluateLoop :: LD.Client -> String -> LD.Context -> Maybe Bool -> Bool -> IO ()
evaluateLoop client featureFlagKey context lastValue showBanner = do
    value <- LD.boolVariation client (pack featureFlagKey) context False
    showBanner' <- showMessage featureFlagKey value lastValue showBanner

    threadDelay (1 * 1_000_000) >> evaluateLoop client featureFlagKey context (Just value) showBanner'

evaluate :: Maybe String -> Maybe String -> IO ()
evaluate (Just sdkKey) Nothing = do evaluate (Just sdkKey) (Just "sample-feature")
evaluate (Just sdkKey) (Just featureFlagKey) = do
    -- Set up the evaluation context. This context should appear on your
    -- LaunchDarkly contexts dashboard soon after you run the demo.
    let context = LD.makeContext "example-user-key" "user" & LD.withName "Sandy"
    client <- LD.makeClient $ LD.makeConfig (pack sdkKey)
    initialized <- timeout (5_000 * 1_000) (waitForClient client)

    case initialized of
        Just True ->  do
            print "*** SDK successfully initialized!"
            evaluateLoop client featureFlagKey context Nothing True
        _notInitialized -> putStrLn "*** SDK failed to initialize. Please check your internet connection and SDK credential for any typo."
evaluate  _ _ = putStrLn "*** You must define LAUNCHDARKLY_SDK_KEY and LAUNCHDARKLY_FLAG_KEY before running this script"

main :: IO ()
main = do
    -- Set sdkKey to your LaunchDarkly SDK key.
    sdkKey <- lookupEnv "LAUNCHDARKLY_SDK_KEY"
    -- Set featureFlagKey to the feature flag key you want to evaluate.
    featureFlagKey <- lookupEnv "my-flag-key"
    evaluate sdkKey featureFlagKey
```

Now that your application is ready, run the application to see what value we get.
```shell
stack build && stack exec hello-haskell-exe
```

You should see:

`Feature flag my-flag-key is FALSE`
