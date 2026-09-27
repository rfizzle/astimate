// shapes exports five types, one an interface: abstractness 1/5.
export interface Shape {
  area: number;
}

export type Point = { x: number; y: number };

export type Id = string;

export enum Kind {
  Square,
  Circle,
}

export class Plain {}
