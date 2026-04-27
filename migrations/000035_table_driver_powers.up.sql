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
-- Name: driver_powers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_powers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    driver_id uuid NOT NULL,
    power_plate_type character varying,
    power_plate_number character varying,
    power_tech_series character varying,
    power_tech_number character varying,
    power_owner_id character varying,
    power_owner_name character varying,
    power_scan_status boolean,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: driver_powers driver_powers_driver_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_powers
    ADD CONSTRAINT driver_powers_driver_id_key UNIQUE (driver_id);


--
-- Name: driver_powers driver_powers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_powers
    ADD CONSTRAINT driver_powers_pkey PRIMARY KEY (id);


--
-- Name: idx_driver_powers_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_powers_driver_id ON public.driver_powers USING btree (driver_id);


--
-- Name: driver_powers driver_powers_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_powers
    ADD CONSTRAINT driver_powers_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


