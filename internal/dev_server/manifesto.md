# Dev Server Vision Doc

This doc lays out our vision for what `dev-server` is supposed to do, and why this is the case.

## Purpose of the Dev Server

First and foremost, `dev-server` is meant as a replacement for local dev and CI environments managed in the LaunchDarkly service. Other use cases may exist (and don't hesitate to ask if you think you have one!), but since a `dev-server` environment lacks any targeting, it is best suited to situations where there is only 1 user for the whole environment.

We implemented it this way in order to solve a problem common both internally and amongst our userbase where an environment would get created per developer on a project as well as per CI build. As you could expect, this becomes exponentially harder to maintain as the number of environments scales both technically and from a process perspective - something has to clear out those environments after all!

Another intentional restriction is that flags must first exist in the source environment. We chose to do this in order to minimize any surprises as your code moves from local / CI to a prod / prod-like environment. In our experience, the greatest source of such surprises has been drift between the configuration of a developer's LD environment and our Staging environment. Unless a developer is constantly making sure their flag state matches higher environments as closely as possible, flag state drifts until their local development environment starts to break, neccessitating a painful process of reconciling flag state.

Lastly, do not use the `dev-server` for environments with prod-level traffic. As we designed this with local dev in mind, production throughput has never been tested.

With that said, it's our hope that you enjoy using the `dev-server`, and feedback is more than welcome!
