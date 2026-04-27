--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: driver_trailers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_trailers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    driver_id uuid NOT NULL,
    trailer_plate_type character varying,
    trailer_plate_number character varying,
    trailer_tech_series character varying,
    trailer_tech_number character varying,
    trailer_owner_id character varying,
    trailer_owner_name character varying,
    trailer_scan_status boolean,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: driver_trailers driver_trailers_driver_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_trailers
    ADD CONSTRAINT driver_trailers_driver_id_key UNIQUE (driver_id);


--
-- Name: driver_trailers driver_trailers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_trailers
    ADD CONSTRAINT driver_trailers_pkey PRIMARY KEY (id);


--
-- Name: idx_driver_trailers_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_trailers_driver_id ON public.driver_trailers USING btree (driver_id);


--
-- Name: driver_trailers driver_trailers_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_trailers
    ADD CONSTRAINT driver_trailers_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


