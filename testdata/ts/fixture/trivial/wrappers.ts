// wrappers shows which declarations the untested directive leaves out.

//astimate:untested thin alias kept for callers
export function wrapped(): number {
  return 1;
}

// astimate:untested with a space is not the directive.
export const spaced = (): number => 2;

//astimate:untested

export function detached(): number {
  return 3;
}

export class Box {
  //astimate:untested
  open(): number {
    return 4;
  }

  close(): number {
    return 5;
  }
}

// listed is exported through a list below.
//astimate:untested
const listed = (): number => 6;

export { listed };
