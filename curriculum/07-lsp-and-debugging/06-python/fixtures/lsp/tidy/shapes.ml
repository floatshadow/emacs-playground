type shape = Circle of float | Square of float | Rect of float*float
let area s = match s with Circle r -> 3.14159 *. r *. r | Square a -> a *. a | Rect (w,h) -> w*.h
let () =   List.iter (fun s -> Printf.printf "%.2f\n" (area s)) [Circle 1.0; Square 2.0; Rect (2.0,3.0)]
