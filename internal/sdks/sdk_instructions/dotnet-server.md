# Installation steps
If you want to skip ahead, the final code is available in our [GitHub repository](https://github.com/launchdarkly/hello-dotnet).

1. Open Visual Studio and create a new C# console application.

2. Next, install the LaunchDarkly SDK using NuGet:
```
Install-Package LaunchDarkly.ServerSdk
```

3. Open the file `Program.cs` and add the following code:
```cs
using System;
  using LaunchDarkly.Sdk;
  using LaunchDarkly.Sdk.Server;

  namespace HelloDotNet
  {
      class Program
      {
          // Set SdkKey to your LaunchDarkly SDK key.
          public const string SdkKey = "1234567890abcdef";

          // Set FeatureFlagKey to the feature flag key you want to evaluate.
          public const string FeatureFlagKey = "my-flag-key";

          private static void ShowMessage(string s) {
              Console.WriteLine("*** " + s);
              Console.WriteLine();
          }

          static void Main(string[] args)
          {
              var ldConfig = Configuration.Default(SdkKey);

              var client = new LdClient(ldConfig);

              if (client.Initialized)
              {
                  ShowMessage("SDK successfully initialized!");
              }
              else
              {
                  ShowMessage("SDK failed to initialize");
                  Environment.Exit(1);
              }

              // Set up the evaluation context. This context should appear on your LaunchDarkly contexts
              // dashboard soon after you run the demo.
              var context = Context.Builder("example-user-key")
                  .Name("Sandy")
                  .Build();

              var flagValue = client.BoolVariation(FeatureFlagKey, context, false);

              ShowMessage(string.Format("Feature flag '{0}' is {1} for this context",
                  FeatureFlagKey, flagValue));

              // Here we ensure that the SDK shuts down cleanly and has a chance to deliver analytics
              // events to LaunchDarkly before the program exits. If analytics events are not delivered,
              // the context attributes and flag usage statistics will not appear on your dashboard. In
              // a normal long-running application, the SDK would continue running and events would be
              // delivered automatically in the background.
              client.Dispose();
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