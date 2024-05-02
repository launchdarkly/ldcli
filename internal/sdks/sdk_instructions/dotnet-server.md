# Installation steps
1. Open Visual Studio and create a new C# console application.

2. Next, install the LaunchDarkly SDK using NuGet:
```
Install-Package LaunchDarkly.ServerSdk
```

3. Open the file `Program.cs` and add the following code:
```cs
using System;
using System.Threading.Tasks;
using LaunchDarkly.Sdk;
using LaunchDarkly.Sdk.Server;

namespace HelloDotNet
{
  class Hello
  {
      public static void ShowBanner(){
          Console.WriteLine(
@"            ██
        ██
    ████████
       ███████
██ LAUNCHDARKLY █
       ███████
    ████████
        ██
      ██
");
      }

      static void Main(string[] args)
      {
          bool CI = Environment.GetEnvironmentVariable("CI") != null;

          string SdkKey = Environment.GetEnvironmentVariable("LAUNCHDARKLY_SDK_KEY");

          // Set FeatureFlagKey to the feature flag key you want to evaluate.
          string FeatureFlagKey = "my-flag-key";

          if (string.IsNullOrEmpty(SdkKey))
          {
              Console.WriteLine("*** Please set LAUNCHDARKLY_SDK_KEY environment variable to your LaunchDarkly SDK key first\n");
              Environment.Exit(1);
          }

          var ldConfig = Configuration.Default(SdkKey);

          var client = new LdClient(ldConfig);

          if (client.Initialized)
          {
              Console.WriteLine("*** SDK successfully initialized!\n");
          }
          else
          {
              Console.WriteLine("*** SDK failed to initialize\n");
              Environment.Exit(1);
          }

          // Set up the evaluation context. This context should appear on your LaunchDarkly contexts
          // dashboard soon after you run the demo.
          var context = Context.Builder("example-user-key")
              .Name("Sandy")
              .Build();

          if (Environment.GetEnvironmentVariable("LAUNCHDARKLY_FLAG_KEY") != null)
          {
              FeatureFlagKey = Environment.GetEnvironmentVariable("LAUNCHDARKLY_FLAG_KEY");
          }

          var flagValue = client.BoolVariation(FeatureFlagKey, context, false);

          Console.WriteLine(string.Format("*** The {0} feature flag evaluates to {1}.\n",
              FeatureFlagKey, flagValue));

          if (flagValue)
          {
              ShowBanner();
          }

          client.FlagTracker.FlagChanged += client.FlagTracker.FlagValueChangeHandler(
              FeatureFlagKey,
              context,
              (sender, changeArgs) => {
                  Console.WriteLine(string.Format("*** The {0} feature flag evaluates to {1}.\n",
                  FeatureFlagKey, changeArgs.NewValue));

                  if (changeArgs.NewValue.AsBool) ShowBanner();
              }
          );

          if(CI) Environment.Exit(0);

          Console.WriteLine("*** Waiting for changes \n");

          Task waitForever = new Task(() => {});
          waitForever.Wait();
      }
  }
}
```

Now that your application is ready, run the application to see what value we get.

If you are using Visual Studio, open HelloDotNet.sln and run the application. Or, to run from the command line, type the following command:
```shell
dotnet run --project HelloDotNet
```
You should see:

`Feature flag my-flag-key is FALSE for this context`
