// Imports tsc does not resolve inside the module: trivial and tested have
// no index file. The relative import counts nowhere, the bare name under
// baseUrl is the npm package "trivial", and the @app/* alias, whose target
// does not resolve, falls through to the npm package "@app/tested".
import "../trivial";
import "trivial";
import "@app/tested";
