(* Small statistics over lists of integers. *)

(* The sum of [xs]. *)
let sum xs = List.fold_left (+) 0 xs

(* The mean of [xs], or [None] for an empty list. *)
let mean xs =
  match xs with
  | [] -> None
  | _ -> Some (float_of_int (sum xs) /. List.length xs)

(* The largest element of [xs], or [None] for an empty list. *)
let largest xs =
  match xs with [] -> None | x :: rest -> List.fold_left max x rest

(* How many elements of [xs] are strictly greater than [t]. *)
let count_above t xs = List.length (List.filter (fun x -> x >= t) xs)

(* [xs] as one line of text: "3, 1, 4". *)
let show xs = String.concat ", " (List.map string_of_int xs)
